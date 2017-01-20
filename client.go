package gohttptun

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"
)

const bufSize = 1024*1024

// take a reader, and turn it into a channel of bufSize chunks of []byte
func makeReadChan(r io.Reader, bufSize int) (chan []byte, chan bool) {
	read := make(chan []byte)
	closed := make(chan bool)
	go func() {
		for {
			b := make([]byte, bufSize)
			n, err := r.Read(b)
			if err != nil {
				closed <- true
				return
			}
			//if n > 0 {
			read <- b[0:n]
			//}
		}
	}()
	return read, closed
}

type ForwardProxy struct {
	listenAddr       string
	remoteProxies    []RemoteProxy
	tickInterval     time.Duration
}

type RemoteProxy struct {
	URL            string
	FrontingDomain string
}

func NewForwardProxy(listenAddr string, remoteProxies []RemoteProxy, tickIntervalMsec int) *ForwardProxy {
	return &ForwardProxy{
		listenAddr:       listenAddr,
		remoteProxies:    remoteProxies,
		tickInterval:     time.Duration(tickIntervalMsec) * time.Millisecond,
	}
}

func (f *ForwardProxy) roundTrip(location string, key string, body io.Reader) (*http.Response, error) {
	proxy := f.remoteProxies[0]
	req, err := http.NewRequest(
		"POST",
		proxy.URL + location,
		body)
	if err != nil {
		log.Println("Error building a request:", err)
		return nil, err
	}
	if proxy.FrontingDomain != "" {
		req.Host = req.URL.Host
		req.URL.Host = proxy.FrontingDomain
	}
	req.Header.Set("Content-type", "application/octet-stream")
	if key != "" {
		req.Header.Set("X-Session-Id", key)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func (f *ForwardProxy) ListenAndServe() error {
	listener, err := net.Listen("tcp", f.listenAddr)
	if err != nil {
		panic(err)
	}
	log.Printf("listen on '%v'", f.listenAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Error accepting connection:", err)
			continue
		}
		log.Println("accept conn", "localAddr.", conn.LocalAddr(), "remoteAddr.", conn.RemoteAddr())

		buf := new(bytes.Buffer)

		seq := 0

		// initiate new session and read key
		log.Println("Attempting connect HttpTun Server.")
		resp, err := f.roundTrip("/create", "", buf)
		if err != nil {
			log.Println("Error connecting to HttpTun server:", err)
			continue
		}
		bkey, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Println("Error wrting to client:", err)
			continue
		}
		key := string(bkey)

		log.Printf("client main(): after Post('/create') we got ResponseWriter with key = '%s'", key)

		// ticker to set a rate at which to hit the server
		tick := time.NewTimer(f.tickInterval)
		read, closed := makeReadChan(conn, bufSize)
		buf.Reset()
		var sendTime, recvTime time.Time
		var sendSize, recvSize int64
		for disconnect := false; !disconnect; {
			select {
			case b := <-read:
				// fill buf here
				po("client: <-read of '%s'; hex:'%x' of length %d added to buffer\n", string(b), b, len(b))
				buf.Write(b)
				po("client: after write to buf of len(b)=%d, buf is now length %d\n", len(b), buf.Len())

			case disconnect = <-closed:
				log.Println("Client closed.")

			case <-tick.C:
			}

			seq++
			po("\n ====================\n client seq = %d\n ====================\n", seq)
			po("client: seq %d, got tick.C. key as always(?) = '%s'. buf is now size %d\n", seq, key, buf.Len())
			resp, err := f.roundTrip("/ping", key, buf)
			if err != nil {
				log.Println("Error sending a request:", err)
				continue
			}
			if resp.Header.Get("X-Session-Id") != key {
				log.Println("Wrong or missing X-Session-Id. Packet skipped.")
				continue
			}
			roundTripTime := time.Now()
			if resp.Request.ContentLength > 0 {
				sendTime = roundTripTime
				sendSize = resp.Request.ContentLength
				log.Printf("sendSize=%d sendTime=%s\n", sendSize, sendTime)
			}

			// write http response response to conn

			n, err := io.Copy(conn, resp.Body)
			if err != nil {
				disconnect = true
			}
			resp.Body.Close()
			if n > 0 {
				recvTime = roundTripTime
				recvSize = n
				log.Printf("recvSize=%d recvTime=%s\n", recvSize, recvTime)
			}

			var nextPoll time.Duration
			switch {
			case roundTripTime.Sub(sendTime) < 5 * time.Second, roundTripTime.Sub(recvTime) < 5 * time.Second:
				nextPoll = f.tickInterval
			case roundTripTime.Sub(sendTime) < 30 * time.Second, roundTripTime.Sub(recvTime) < 30 * time.Second:
				nextPoll = 5 * time.Second
			case roundTripTime.Sub(sendTime) < 300 * time.Second, roundTripTime.Sub(recvTime) < 300 * time.Second:
				nextPoll = 30 * time.Second
			default:
				nextPoll = 60 * time.Second
			}
			log.Println("nextPoll=", nextPoll)
			tick.Stop()
			tick.Reset(nextPoll)
		}
		buf.Reset()
		resp, err = f.roundTrip("/close", key, buf)
	}
}

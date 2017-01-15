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
	revProxyAddr     string
	tickIntervalMsec int
}

func NewForwardProxy(listenAddr string, revProxAddr string, tickIntervalMsec int) *ForwardProxy {
	return &ForwardProxy{
		listenAddr:       listenAddr,
		revProxyAddr:     revProxAddr, // http server's address
		tickIntervalMsec: tickIntervalMsec,
	}
}

func (f *ForwardProxy) ListenAndServe() error {
	listener, err := net.Listen("tcp", f.listenAddr)
	if err != nil {
		panic(err)
	}
	log.Printf("listen on '%v', with revProxAddr '%v'", f.listenAddr, f.revProxyAddr)

	conn, err := listener.Accept()
	if err != nil {
		panic(err)
	}
	log.Println("accept conn", "localAddr.", conn.LocalAddr(), "remoteAddr.", conn.RemoteAddr())

	buf := new(bytes.Buffer)

	sendCount := 0

	// initiate new session and read key
	log.Println("Attempting connect HttpTun Server.", f.revProxyAddr)
	//buf.Write([]byte(*destAddr))
	resp, err := http.Post(
		"http://"+f.revProxyAddr+"/create",
		"text/plain",
		buf)
	panicOn(err)
	bkey, err := ioutil.ReadAll(resp.Body)
	panicOn(err)
	resp.Body.Close()
	key := string(bkey)

	log.Printf("client main(): after Post('/create') we got ResponseWriter with key = '%s'", key)

	// ticker to set a rate at which to hit the server
	tick := time.NewTicker(time.Duration(int64(f.tickIntervalMsec)) * time.Millisecond)
	read, closed := makeReadChan(conn, bufSize)
	buf.Reset()
	for {
		select {
		case b := <-read:
			// fill buf here
			po("client: <-read of '%s'; hex:'%x' of length %d added to buffer\n", string(b), b, len(b))
			buf.Write(b)
			po("client: after write to buf of len(b)=%d, buf is now length %d\n", len(b), buf.Len())

		case <-closed:
			log.Println("Client closed.")
			return nil

		case <-tick.C:
			sendCount++
			po("\n ====================\n client sendCount = %d\n ====================\n", sendCount)
			po("client: sendCount %d, got tick.C. key as always(?) = '%x'. buf is now size %d\n", sendCount, key, buf.Len())
			req, err := http.NewRequest(
				"POST",
				"http://"+f.revProxyAddr+"/ping",
				buf)
			req.Header.Set("Content-type", "application/octet-stream")
			req.Header.Set("X-Session-Id", key)
			panicOn(err)
			resp, err := http.DefaultTransport.RoundTrip(req)
			if err != nil && err != io.EOF {
				log.Println(err.Error())
				continue
			}
			if resp.Header.Get("X-Session-Id") != key {
				log.Println("Wrong or missing X-Session-Id. Packet skipped.")
				continue
			}

			// write http response response to conn

			_, err = io.Copy(conn, resp.Body)
			panicOn(err)
			resp.Body.Close()
		}
	}
}

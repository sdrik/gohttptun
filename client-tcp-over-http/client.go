package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	tun "github.com/glycerine/gohttptun"
)

// print out shortcut
var po = tun.VPrintf

const bufSize = 1024

var (
	listenAddr   = flag.String("listen", ":2222", "local listen address")
	httpAddr     = flag.String("http", fmt.Sprintf("%s:%d", tun.ReverseProxyIp, tun.ReverseProxyPort), "remote tunnel server")
	tickInterval = flag.Int("tick", 250, "update interval (msec)") // orig: 250
)

// take a reader, and turn it into a channel of bufSize chunks of []byte
func makeReadChan(r io.Reader, bufSize int) chan []byte {
	read := make(chan []byte)
	go func() {
		for {
			b := make([]byte, bufSize)
			n, err := r.Read(b)
			if err != nil {
				return
			}
			//if n > 0 {
			read <- b[0:n]
			//}
		}
	}()
	return read
}

func main() {
	flag.Parse()
	log.SetPrefix("tun.client: ")

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		panic(err)
	}
	log.Printf("listen on '%v', with httpAddr '%v'", *listenAddr, *httpAddr)

	conn, err := listener.Accept()
	if err != nil {
		panic(err)
	}
	log.Println("accept conn", "localAddr.", conn.LocalAddr(), "remoteAddr.", conn.RemoteAddr())

	buf := new(bytes.Buffer)

	sendCount := 0

	// initiate new session and read key
	log.Println("Attempting connect HttpTun Server.", *httpAddr)
	//buf.Write([]byte(*destAddr))
	resp, err := http.Post(
		"http://"+*httpAddr+"/create",
		"text/plain",
		buf)
	panicOn(err)
	key, err := ioutil.ReadAll(resp.Body)
	panicOn(err)
	resp.Body.Close()

	log.Printf("client main(): after Post('/create') we got ResponseWriter with key = '%x'", key)

	// ticker to set a rate at which to hit the server
	tick := time.NewTicker(time.Duration(int64(*tickInterval)) * time.Millisecond)
	read := makeReadChan(conn, bufSize)
	buf.Reset()
	for {
		select {
		case b := <-read:
			// fill buf here
			po("client: <-read of '%s'; hex:'%x' of length %d added to buffer\n", string(b), b, len(b))
			buf.Write(b)
			po("client: after write to buf of len(b)=%d, buf is now length %d\n", len(b), buf.Len())

		case <-tick.C:
			sendCount++
			po("\n ====================\n client sendCount = %d\n ====================\n", sendCount)
			po("client: sendCount %d, got tick.C. key as always(?) = '%x'. buf is now size %d\n", sendCount, key, buf.Len())
			// write buf to new http request, starting with key
			req := bytes.NewBuffer(key)
			buf.WriteTo(req)
			resp, err := http.Post(
				"http://"+*httpAddr+"/ping",
				"application/octet-stream",
				req)
			if err != nil && err != io.EOF {
				log.Println(err.Error())
				continue
			}

			// write http response response to conn

			// we take apart the io.Copy to print out the response for debugging.
			//_, err = io.Copy(conn, resp.Body)

			body, err := ioutil.ReadAll(resp.Body)
			panicOn(err)
			po("client: resp.Body = '%s'\n", string(body))
			_, err = conn.Write(body)
			panicOn(err)
			resp.Body.Close()
		}
	}
}

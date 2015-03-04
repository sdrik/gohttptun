package main

import (
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

//var destAddr = "127.0.0.1:12222" // tunnel destination
var destAddr = "127.0.0.1:22" // tunnel destination
//var destAddr = "127.0.0.1:1234" // tunnel destination

type Server struct {
	destIP   string
	destPort string
	destAddr string
}

const (
	readTimeoutMsec = 1000
	//keyLen          = 64
	keyLen = 6
)

type proxy struct {
	C         chan proxyPacket
	key       string
	conn      net.Conn
	recvCount int
}

type proxyPacket struct {
	resp    http.ResponseWriter
	request *http.Request
	body    []byte
	done    chan bool
}

// print out shortcut
var po = tun.VPrintf

func NewProxy(key, destAddr string) (p *proxy, err error) {
	po("starting with NewProxy\n")
	p = &proxy{C: make(chan proxyPacket), key: key, recvCount: 0}
	log.Println("Attempting connect", destAddr)
	p.conn, err = net.Dial("tcp", destAddr)
	panicOn(err)

	err = p.conn.SetReadDeadline(time.Now().Add(time.Millisecond * readTimeoutMsec))
	panicOn(err)

	log.Println("ResponseWriter directed to ", destAddr)
	po("done with NewProxy\n")
	return
}

func (p *proxy) handle(pp proxyPacket) {
	p.recvCount++
	po("\n ====================\n server proxy.recvCount = %d    len(pp.body)= %d\n ================\n", p.recvCount, len(pp.body))

	po("in proxy::handle(pp) with pp = '%#v'\n", pp)
	// read from the request body and write to the ResponseWriter
	writeMe := pp.body[keyLen:]
	n, err := p.conn.Write(writeMe)
	if n != len(writeMe) {
		log.Printf("proxy::handle(pp): could only write %d of the %d bytes to the connection. err = '%v'", n, len(pp.body), err)
	} else {
		po("proxy::handle(pp): wrote all %d bytes of writeMe to the final (sshd server) connection: '%s'.", len(writeMe), string(writeMe))
	}
	pp.request.Body.Close()
	if err == io.EOF {
		p.conn = nil
		log.Printf("proxy::handle(pp): EOF for key '%x'", p.key)
		return
	}
	// read out of the buffer and write it to conn
	pp.resp.Header().Set("Content-type", "application/octet-stream")
	// temp for debug: n64, err := io.Copy(pp.resp, p.conn)

	b500 := make([]byte, 500)

	err = p.conn.SetReadDeadline(time.Now().Add(time.Millisecond * readTimeoutMsec))
	panicOn(err)

	n64, err := p.conn.Read(b500)
	if err != nil {
		// i/o timeout expected
	}
	po("\n\n server got reply from p.conn of len %d: '%s'\n", n64, string(b500[:n64]))
	_, err = pp.resp.Write(b500[:n64])
	if err != nil {
		panic(err)
	}

	// don't panicOn(err)
	log.Printf("proxy::handle(pp): io.Copy into pp.resp from p.conn moved %d bytes", n64)
	pp.done <- true
	po("proxy::handle(pp) done.\n")
}

var queue = make(chan proxyPacket)
var createQueue = make(chan *proxy)

func handler(c http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	panicOn(err)
	po("top level handler(): in '/' and '/ping' handler, packet len without key: %d: making new proxyPacket, http.Request r = '%#v'. r.Body = '%s'\n", len(body)-keyLen, *r, string(body))

	pp := proxyPacket{
		resp:    c,
		request: r,
		body:    body, // includes key of keyLen in prefix
		done:    make(chan bool),
	}
	queue <- pp
	<-pp.done // wait until done before returning, as this will return anything written to c to the client.
}

func (s *Server) createHandler(c http.ResponseWriter, r *http.Request) {
	// fix destAddr on server side to prevent being a transport for other actions.

	// destAddr used to be here, but no more. Still have to close the body.
	_, err := ioutil.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(c, "Could not read destAddr",
			http.StatusInternalServerError)
		return
	}

	key := genKey()
	po("in createhandler(): Server::createHandler generated key '%s'\n", key)

	p, err := NewProxy(key, s.destAddr)
	if err != nil {
		http.Error(c, "Could not connect",
			http.StatusInternalServerError)
		return
	}
	po("Server::createHandler about to send createQueue <- p, where p = %p\n", p)
	createQueue <- p
	po("Server::createHandler(): sent createQueue <- p.\n")

	c.Write([]byte(key))
	po("Server::createHandler done.\n")
}

func proxyMuxer() {
	po("proxyMuxer started\n")
	proxyMap := make(map[string]*proxy)
	for {
		select {
		case pp := <-queue:
			key := make([]byte, keyLen)
			// read key
			//n, err := pp.req.Body.Read(key)
			if len(pp.body) < keyLen {
				log.Printf("Couldn't read key, not enough bytes in body. len(body) = %d\n", len(pp.body))
				continue
			}
			copy(key, pp.body)

			po("proxyMuxer: from pp <- queue, we read key '%x'\n", key)
			// find proxy
			p, ok := proxyMap[string(key)]
			if !ok {
				log.Printf("Couldn't find proxy for key = '%x'", key)
				continue
			}
			// handle
			po("proxyMuxer found proxy for key '%x'\n", key)
			p.handle(pp)
		case p := <-createQueue:
			po("proxyMuxer: got p=%p on <-createQueue\n", p)
			proxyMap[p.key] = p
			po("proxyMuxer: after adding key '%x', proxyMap is now: '%#v'\n", p.key, proxyMap)
		}
	}
	po("proxyMuxer done\n")
}

var httpAddr = flag.String("http", fmt.Sprintf("%s:%d", tun.ReverseProxyIp, tun.ReverseProxyPort), "http listen address")

func main() {
	flag.Parse()

	s := &Server{destAddr: destAddr}

	go proxyMuxer()

	http.HandleFunc("/", handler)
	http.HandleFunc("/create", s.createHandler)
	fmt.Printf("about to ListenAndServer on httpAddr'%#v'. Ultimate destAddr: '%s'\n", *httpAddr, destAddr)
	err := http.ListenAndServe(*httpAddr, nil)
	if err != nil {
		panic(err)
	}
}

func genKey() string {
	return "key123"
	/*
		key := make([]byte, keyLen)
		for i := 0; i < keyLen; i++ {
			key[i] = byte(rand.Int())
		}
		return string(key)
	*/
}

/*
Copyright 2013 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"
)

var destAddr = "127.0.0.1:22" // tunnel destination

type Server struct {
	destIP   string
	destPort string
	destAddr string
}

const (
	readTimeoutMsec = 12000
	keyLen          = 64
)

type proxy struct {
	C    chan proxyPacket
	key  string
	conn net.Conn
}

type proxyPacket struct {
	resp    http.ResponseWriter
	request *http.Request
	body    []byte
	done    chan bool
}

// print out shortcut
var po = fmt.Printf

func NewProxy(key, destAddr string) (p *proxy, err error) {
	po("starting with NewProxy\n")
	p = &proxy{C: make(chan proxyPacket), key: key}
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
	po("in proxy::handle(pp) with pp = '%#v'\n", pp)
	// read from the request body and write to the ResponseWriter
	n, err := p.conn.Write(pp.body)
	if n != len(pp.body) {
		log.Printf("proxy::handle(pp): could only write %d of the %d bytes to the connection. err = '%v'", n, len(pp.body), err)
	}
	pp.request.Body.Close()
	if err == io.EOF {
		p.conn = nil
		log.Printf("proxy::handle(pp): EOF for key '%x'", p.key)
		return
	}
	// read out of the buffer and write it to conn
	pp.resp.Header().Set("Content-type", "application/octet-stream")
	n64, err := io.Copy(pp.resp, p.conn)
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
	po("top level handler(): in '/' and '/ping' and everything-not-'/create': making new proxyPacket, http.Request r = '%#v'. r.Body = '%s'\n", *r, string(body))

	pp := proxyPacket{
		resp:    c,
		request: r,
		body:    body,
		done:    make(chan bool),
	}
	queue <- pp
	<-pp.done // wait until done before returning, as this will return anything written to c to the client.
}

func (s *Server) createHandler(c http.ResponseWriter, r *http.Request) {
	// fix destAddr on server side to prevent being a transport for other actions.

	/*
		// read destAddr
		destAddr, err := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			http.Error(c, "Could not read destAddr",
				http.StatusInternalServerError)
			return
		}
	*/

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

var httpAddr = flag.String("http", ":8888", "http listen address")

func main() {
	flag.Parse()

	s := &Server{destAddr: destAddr}

	go proxyMuxer()

	http.HandleFunc("/", handler)
	http.HandleFunc("/create", s.createHandler)
	fmt.Printf("about to ListenAndServer on httpAddr'%#v'\n", *httpAddr)
	http.ListenAndServe(*httpAddr, nil)
}

func genKey() string {
	key := make([]byte, keyLen)
	for i := 0; i < keyLen; i++ {
		key[i] = byte(rand.Int())
	}
	return string(key)
}

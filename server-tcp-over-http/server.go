package main

import (
	"flag"
	"fmt"

	tun "github.com/sdrik/gohttptun"
)

//var destAddr = "127.0.0.1:12222" // tunnel destination
var destAddr = "127.0.0.1:22" // tunnel destination
//var destAddr = "127.0.0.1:1234" // tunnel destination

var listenAddr = flag.String("http", fmt.Sprintf("%s:%d", tun.ReverseProxyIp, tun.ReverseProxyPort), "http listen address")

var verbose = flag.Bool("verbose", false, "verbose")

func main() {
	flag.Parse()
	tun.Verbose = *verbose

	s := tun.NewReverseProxy(*listenAddr, destAddr)
	s.ListenAndServe()
}

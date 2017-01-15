package main

import (
	"flag"
	"fmt"
	"log"

	tun "github.com/sdrik/gohttptun"
)

// print out shortcut
var po = tun.VPrintf

const bufSize = 1024

var (
	verbose      = flag.Bool("verbose", false, "verbose")
	listenAddr   = flag.String("listen", ":2222", "local listen address")
	httpAddr     = flag.String("http", fmt.Sprintf("http://%s:%d", tun.ReverseProxyIp, tun.ReverseProxyPort), "remote tunnel server")
	tickInterval = flag.Int("tick", 250, "update interval (msec)") // orig: 250
)

func main() {
	flag.Parse()
	tun.Verbose = *verbose
	log.SetPrefix("tun.client: ")

	f := tun.NewForwardProxy(*listenAddr, *httpAddr, *tickInterval)
	f.ListenAndServe()
}

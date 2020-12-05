package main

import (
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/proxy"
)

func handleHTTP(w http.ResponseWriter, req *http.Request, dialer proxy.Dialer) {
	tp := http.Transport{
		Dial: dialer.Dial,
	}
	resp, err := tp.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func handleTunnel(w http.ResponseWriter, req *http.Request, dialer proxy.Dialer) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	srcConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	dstConn, err := dialer.Dial("tcp", req.Host)
	if err != nil {
		srcConn.Close()
		return
	}

	srcConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	go transfer(dstConn, srcConn)
	go transfer(srcConn, dstConn)
}

func transfer(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()

	io.Copy(dst, src)
}

var (
	socks5Addr string
	addr       string
	staticDir  string
	hostname   string

	handleStaticDir http.Handler
)

func serveHTTP(w http.ResponseWriter, req *http.Request) {
	// skip local hostname (serve static files)
	if req.Host == hostname {
		if handleStaticDir == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		handleStaticDir.ServeHTTP(w, req)
		return
	}

	d := &net.Dialer{
		Timeout: 10 * time.Second,
	}
	dialer, _ := proxy.SOCKS5("tcp", socks5Addr, nil, d)

	if req.Method == "CONNECT" {
		handleTunnel(w, req, dialer)
	} else {
		handleHTTP(w, req, dialer)
	}
}

func main() {
	flag.StringVar(&socks5Addr, "s", "", "socks5 addr")
	flag.StringVar(&addr, "l", "", "listen addr")
	flag.StringVar(&staticDir, "w", "", "serve static files")
	flag.StringVar(&hostname, "h", "", "host name")
	flag.Parse()

	if socks5Addr == "" || addr == "" || hostname == "" {
		flag.Usage()
		os.Exit(1)
	}
	if staticDir != "" {
		handleStaticDir = http.FileServer(http.Dir(staticDir))
	}

	err := http.ListenAndServe(addr, http.HandlerFunc(serveHTTP))
	if err != nil {
		log.Fatal(err)
	}
}

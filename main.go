package main

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
)

type (
	ResponseMatcher struct {
		Path string
	}

	ResponseOverride struct {
		StatusCode int
		Body       []byte
		Header     http.Header
	}

	Proxy struct {
		host      string
		proxy     *httputil.ReverseProxy
		overrides map[ResponseMatcher]ResponseOverride
	}
)

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path + "?" + r.URL.Query().Encode()
	for matcher, override := range p.overrides {
		matchedUrl, err := url.Parse(matcher.Path)
		if err != nil {
			log.Println("error parsing matcher path", err)
			continue
		}

		matchedPath := matchedUrl.Path + "?" + matchedUrl.Query().Encode()
		if strings.HasPrefix(urlPath, matchedPath) {
			log.Println("overriding", urlPath)

			for key, value := range override.Header {
				w.Header()[key] = value
			}
			w.Write(override.Body)
			w.WriteHeader(override.StatusCode)
			return
		}
	}

	log.Println("proxying", urlPath)
	r.Host = p.host
	p.proxy.ServeHTTP(w, r)
}

func (p *Proxy) AddOverride(matcher ResponseMatcher, override ResponseOverride) {
	p.overrides[matcher] = override
}

func NewProxy(host string, target string) *Proxy {
	targetURL, err := url.Parse("https://" + target)
	if err != nil {
		panic(err)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &Proxy{host: host, proxy: proxy, overrides: make(map[ResponseMatcher]ResponseOverride)}
}

func OverrideJSON(status int, path string, options ...string) ResponseOverride {
	contentType := "application/json"
	if len(options) > 0 {
		contentType = options[0]
	}

	body, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	header := http.Header{}
	header.Add("Content-Type", contentType)

	return ResponseOverride{StatusCode: status, Body: body, Header: header}
}

func main() {
	listenAddr := ":8443"
	proxyHost := "www.linkedin.com"
	proxyTarget := "13.107.42.14"

	if len(os.Args) > 1 {
		listenAddr = os.Args[1]
	}

	if len(os.Args) > 2 {
		proxyHost = os.Args[2]
	}

	if len(os.Args) > 3 {
		proxyTarget = os.Args[3]
	}

	// Replace 'target' with the URL of the server you want to proxy to
	target, err := url.Parse("https://" + proxyTarget)
	if err != nil {
		panic(err)
	}

	// Create a new ReverseProxy instance
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Configure the reverse proxy to use HTTPS
	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	// Create a new Proxy instance
	p := NewProxy(proxyHost, proxyTarget)
	p.AddOverride(
		ResponseMatcher{
			Path: "/voyager/api/graphql?includeWebMetadata=true&queryId=voyagerIdentityDashProfileComponents.7af5d6f176f11583b382e37e5639e69e&variables=(profileUrn:urn:li:fsd_profile:ACoAAB8IsF0BbnNIqr5d0Ol_EBoLt1ns2BXdoAg,sectionType:skills",
		},
		OverrideJSON(200, "response_body.json", "application/vnd.linkedin.normalized+json+2.1; charset=UTF-8"),
	)

	server := &http.Server{
		Addr:    listenAddr,
		Handler: p,
	}

	// handle sigint
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		log.Println("shutting down")
		server.Shutdown(context.Background())
	}()

	// Start the HTTP server and register the Proxy instance as the handler
	log.Println("starting server on", listenAddr)
	err = server.ListenAndServeTLS("server.crt", "server.key")
	if err == http.ErrServerClosed {
		log.Println("stopped")
	} else if err != nil {
		panic(err)
	}

}

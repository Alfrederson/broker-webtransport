package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

type Client struct {
}

type Server struct {
	clients map[*webtransport.Session]Client
	mutex   sync.Mutex
}

func (s *Server) handleSession(sess *webtransport.Session) {
	defer func() {
		s.mutex.Lock()
		delete(s.clients, sess)
		s.mutex.Unlock()
		sess.CloseWithError(1234, "sai.")
	}()

	for {
		stream, err := sess.AcceptStream(context.Background())
		if err != nil {
			log.Println("ERRO: sess.AcceptStream: ", err)
			return
		}
		go s.handleStream(stream, sess)
	}
}

func (s *Server) handleStream(stream webtransport.Stream, sess *webtransport.Session) {
	buf := make([]byte, 1024)
	for {
		n, err := stream.Read(buf)
		if err != nil {
			log.Println("ERRO: stream.Read: ", err)
			return
		}
		msg := buf[:n]
		fmt.Println("< ", msg)
		s.broadcast(msg, sess)
	}
}

func (s *Server) broadcast(msg []byte, sender *webtransport.Session) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for client := range s.clients {
		if client != sender {
			stream, err := client.OpenStreamSync(context.Background())
			if err != nil {
				log.Println("ERRO: client.OpenStreamSync ", err)
				continue
			}
			stream.Write(msg)
			stream.Close()
		}
	}
}

const certFile = "/etc/letsencrypt/live/broker.r718.org/fullchain.pem" // Replace with the path to your certificate file
const keyFile = "/etc/letsencrypt/live/broker.r718.org/privkey.pem"    // Replace with the path to your key file

func generateTLSConfig() *tls.Config {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "aoo, desgraça!")
}

func main() {

	server := &Server{
		clients: make(map[*webtransport.Session]Client),
	}

	wt_server := webtransport.Server{
		H3: http3.Server{
			Addr:      ":443",
			TLSConfig: generateTLSConfig(),
		},
	}
	http.HandleFunc("/", helloHandler)

	http.HandleFunc("/wt", func(w http.ResponseWriter, r *http.Request) {
		log.Println("recebi pedido")
		sess, err := wt_server.Upgrade(w, r)
		if err != nil {
			log.Printf("ERRO: wt_server.Upgrade: ", err)
			w.WriteHeader(500)
			return
		}

		server.handleSession(sess)
	})

	log.Println("inicializando...")
	// log.Fatal(wt_server.ListenAndServe())
	log.Fatal(wt_server.ListenAndServeTLS(certFile, keyFile))
}

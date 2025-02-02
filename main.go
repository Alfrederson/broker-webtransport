package main

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

const (
	certFile = "/etc/letsencrypt/live/broker.r718.org/fullchain.pem" // Replace with the path to your certificate file
	keyFile  = "/etc/letsencrypt/live/broker.r718.org/privkey.pem"   // Replace with the path to your key file
)

var wtConfig = webtransport.Server{
	H3: http3.Server{
		Addr:      "3122",
		TLSConfig: generateTLSConfig(),
	},
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var (
	public  = make(chan []byte)
	clients = make(map[chan<- []byte]bool)
)

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

func main() {
	go serveFrontend()
	go broadcaster()
	serveWebtransport()
}

func broadcaster() {
	for message := range public {
		for client := range clients {
			client <- message
		}
	}
}

func sendMessages(stream webtransport.Stream, outgoing <-chan []byte) {
	for message := range outgoing {
		stream.Write(message)
	}
}

func readMessages(stream webtransport.Stream, public chan<- []byte, name string) error {
	message := make([]byte, 80)
	for {
		n, err := stream.Read(message)
		if err != nil && err != io.EOF {
			return err
		}
		log.Println("recebi %d bytes do stream %s", n, stream.StreamID())
		public <- message
		if err != io.EOF {
			return err
		}
	}
}

func handleWTConn(w http.ResponseWriter, r *http.Request) {
	session, err := wtConfig.Upgrade(w, r)
	if err != nil {
		log.Println("ERRO: wtConfig.Upgrade: ", err)
		return
	}
	log.Printf("sessão aberta para %s\n", session.RemoteAddr())

	stream, err := session.OpenStream()
	if err != nil {
		log.Println("ERRO: session.OpensTream: ", err)
		return
	}
	log.Printf("stream aberta %s\n", stream.StreamID())

	outgoing := make(chan []byte)
	go sendMessages(stream, outgoing)

	outgoing <- []byte("bem vindo")
	clients[outgoing] = true
	err = readMessages(stream, public, session.RemoteAddr().String())
	if err != nil {
		delete(clients, outgoing)
		close(outgoing)
		stream.CancelWrite(0)
		log.Printf("stream %s fechada porque %v", stream.StreamID(), err)
	}
}

func serveFrontend() {
	log.Println("iniciando um servidor web...")
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world!"))
	})
	http.HandleFunc("/wt", handleWTConn)

	err := http.ListenAndServeTLS(":443", certFile, keyFile, nil)
	if err != nil {
		panic(err)
	}
}

func serveWebtransport() {
	log.Println("iniciando o webtransport...")
	err := wtConfig.ListenAndServeTLS(certFile, keyFile)
	if err != nil {
		panic(err)
	}
}

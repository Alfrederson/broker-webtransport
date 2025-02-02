package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

const (
	certFile = "/etc/letsencrypt/live/broker.r718.org/fullchain.pem" // Replace with the path to your certificate file
	keyFile  = "/etc/letsencrypt/live/broker.r718.org/privkey.pem"   // Replace with the path to your key file
)

var wtConfig = webtransport.Server{
	H3: http3.Server{
		Addr:      ":3122",
		TLSConfig: generateTLSConfig(),
	},
	CheckOrigin: func(r *http.Request) bool {
		log.Println("checando origem ", r.Header["Origin"])
		return true
	},
}

var (
	public  = make(chan []byte)
	clients = make(map[chan<- []byte]bool)
)

func generateCertificate(subject []string) (tls.Certificate, error) {
	var certificate tls.Certificate
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return certificate, err
	}
	serialNumber, err := rand.Int(rand.Reader, big.NewInt(0x7FFFFFFF))
	if err != nil {
		return certificate, err
	}
	var certTemplate = x509.Certificate{
		SignatureAlgorithm: x509.ECDSAWithSHA256,
		PublicKeyAlgorithm: x509.ECDSA,
		NotAfter:           time.Now().Add(24 * time.Hour),
		DNSNames:           subject,
		SerialNumber:       serialNumber,
	}
	rawCertificate, err := x509.CreateCertificate(rand.Reader, &certTemplate, &certTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		return certificate, err
	}
	// cert, err := x509.ParseCertificate(rawCertificate)
	// log.Println(base64.StdEncoding.EncodeToString(cert.Signature))
	return tls.Certificate{
		Certificate:                  [][]byte{rawCertificate},
		PrivateKey:                   privateKey,
		SupportedSignatureAlgorithms: []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256},
	}, nil
}

func generateTLSConfig() *tls.Config {
	cert, err := generateCertificate([]string{"broker.r718.org"})
	if err != nil {
		panic(err)
	}
	// cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
}

func main2() {
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
	log.Println("pedido recebido")
	log.Println("tentando fazer upgrade")
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
	err := wtConfig.ListenAndServe()
	if err != nil {
		panic(err)
	}
}

func main() {
	s := webtransport.Server{
		H3: http3.Server{
			Addr:      "443",
			TLSConfig: generateTLSConfig(),
		},
	}

	http.HandleFunc("/wt", func(w http.ResponseWriter, r *http.Request) {
		log.Println("opa!")

		_, err := s.Upgrade(w, r)
		if err != nil {
			log.Printf("upgrade falhou %s", err)
			w.WriteHeader(500)
			return
		}

		log.Println("conexão ok!")
	})

	s.ListenAndServeTLS(certFile, keyFile)
}

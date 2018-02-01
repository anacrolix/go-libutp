package main

import (
	"io"
	"log"
	"net"
	"os"
	"time"

	_ "github.com/anacrolix/envpprof"
	"github.com/anacrolix/tagflag"

	"github.com/anacrolix/go-libutp"
)

func getConn(listen bool, addr string, s *utp.Socket) net.Conn {
	if listen {
		c, err := s.Accept()
		if err != nil {
			panic(err)
		}
		return c
	} else {
		c, err := s.Dial(addr)
		if err != nil {
			panic(err)
		}
		return c
	}
}

func main() {
	log.SetFlags(log.Lshortfile | log.Flags())
	var flags = struct {
		Listen bool `name:"l"`
		tagflag.StartPos
		Addr string
	}{}
	tagflag.Parse(&flags)
	s, err := func() (*utp.Socket, error) {
		if flags.Listen {
			return utp.NewSocket("udp", flags.Addr)
		} else {
			return utp.NewSocket("udp", ":0")
		}
	}()
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	c, err := func() (net.Conn, error) {
		if flags.Listen {
			return s.Accept()
		} else {
			return s.Dial(flags.Addr)
		}
	}()
	if err != nil {
		log.Fatal(err)
	}
	doneReading := make(chan struct{})
	doneWriting := make(chan struct{})
	go func() {
		defer close(doneReading)
		n, err := io.Copy(os.Stdout, c)
		log.Printf("read %d bytes then got error: %v", n, err)
	}()
	go func() {
		defer close(doneWriting)
		n, err := io.Copy(c, os.Stdin)
		log.Printf("wrote %d bytes then got error: %v", n, err)
	}()
	select {
	case <-doneReading:
	case <-doneWriting:
	}
	c.Close()
	time.Sleep(time.Second)
}

package main

import (
	"io"
	"log"
	"net"
	"os"
	"sync"

	_ "github.com/anacrolix/envpprof"

	"github.com/anacrolix/go-libutp"
	"github.com/anacrolix/tagflag"
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
	defer c.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer c.Close()
		log.Println(io.Copy(os.Stdout, c))
	}()
	go func() {
		defer wg.Done()
		log.Println(io.Copy(c, os.Stdin))
		c.Close()
	}()
	wg.Wait()
}

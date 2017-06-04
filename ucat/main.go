package main

import (
	"io"
	"log"
	"net"
	"os"
	"sync"

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
		Listen bool
		tagflag.StartPos
		// Addr string
	}{}
	tagflag.Parse(&flags)
	s := utp.NewSocket(func() int {
		if flags.Listen {
			return 3000
		} else {
			return 0
		}
	}())
	c := getConn(flags.Listen, "localhost:3000", s)
	defer c.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		log.Println(io.Copy(os.Stdout, c))
	}()
	go func() {
		defer wg.Done()
		log.Println(io.Copy(c, os.Stdin))
	}()
	wg.Wait()
}

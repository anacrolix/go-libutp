package main

import (
	"io"
	"log"
	"os"

	"github.com/anacrolix/libutp"
)

func main() {
	log.SetFlags(log.Lshortfile | log.Flags())
	s := utp.NewSocket()
	c, err := s.Accept()
	if err != nil {
		panic(err)
	}
	log.Printf("accepted conn from %s", c.RemoteAddr())
	defer c.Close()
	io.Copy(os.Stdout, c)
}

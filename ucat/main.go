package main

import (
	"github.com/anacrolix/libutp"
)

func main() {
	s := utp.NewSocket()
	s.Accept()
}

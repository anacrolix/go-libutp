//go:build !cgo || purego

package utp

// Default is the implementation this binary was built with: the pure Go one here, because the
// build set the purego tag or disabled cgo. Otherwise it would be Libutp. A program that wants to
// override the build's choice can assign to it before making any sockets.
var Default = Purego

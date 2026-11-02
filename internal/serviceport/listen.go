// Package serviceport reports listener failures without choosing another port.
package serviceport

import (
	"fmt"
	"net"
)

type OccupiedError struct {
	Address string
	Cause   error
}

func (e *OccupiedError) Error() string {
	_, port, _ := net.SplitHostPort(e.Address)
	return fmt.Sprintf("端口 %s 已被占用（%s）。请在“本机设备 → 启动设置”中修改监听端口后重试。", port, e.Address)
}

func (e *OccupiedError) Unwrap() error { return e.Cause }

func Listen(address string) (net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	if occupied(err) {
		return nil, &OccupiedError{Address: address, Cause: err}
	}
	return listener, err
}

func Check(address string) error {
	listener, err := Listen(address)
	if err != nil {
		return err
	}
	return listener.Close()
}

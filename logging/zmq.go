package logging

//https://github.com/go-zeromq/zmq4/blob/main/example/psenvpub.go

import (
	"github.com/pebbe/zmq4"
)

type ZmqHandler struct {
	mxAddr string
	topic  string

	sock *zmq4.Socket
	//sock protocol.Socket
	//sock mangos.Socket
	//sock mangos.Socket
}

func NewZmqHandler(mxAddr string, topic string) *ZmqHandler {
	handle := &ZmqHandler{mxAddr: mxAddr, topic: topic}
	handle.sock, _ = zmq4.NewSocket(zmq4.PUB)

	//handle.sock, _ = sub.NewSocket()
	// 堵塞式的 Dial , 不行
	if err := handle.sock.Connect(mxAddr); err != nil {
		//if err := handle.sock.Dial(mxAddr); err != nil {
		return nil
	}

	return handle
}

func (z *ZmqHandler) Emit(message string) {
	_, _ = z.sock.Send(message, zmq4.DONTWAIT)

}

func (z *ZmqHandler) Write(p []byte) (n int, err error) {
	n, err = z.sock.SendBytes(p, zmq4.DONTWAIT)
	//err = z.sock.Send(p)
	//n = len(p)
	return
}

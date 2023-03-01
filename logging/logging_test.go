package logging

import (
	"io"
	"log"
	"os"
	"testing"
)

func TestLogBasic(t *testing.T) {
	fn := "test.log"
	fp, _ := os.OpenFile(fn, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	logger := log.New(io.MultiWriter(os.Stdout, fp,
		NewZmqHandler("tcp://127.0.0.1:1911", "test")),
		"", log.LstdFlags|log.Lshortfile)

	logger = log.New(io.MultiWriter(os.Stdout, fp,
		//NewZmqHandler("tcp://127.0.0.1:1911", "test")),
		NewZmqHandler("ipc://abc", "test")),
		"", 0)

	//log.SetOutput(io.MultiWriter(os.Stdout, fp))
	//log.SetPrefix("Ukrainian ")
	//log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Println("this is message")
}

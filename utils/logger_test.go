package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"
)

func Test_logger(test *testing.T) {

	fn := "abc.log"
	format := "2006-01-02"
	t := time.Now().UTC().Format(format)
	fn = fmt.Sprintf("%s_%s", fn, t)
	writers := []io.Writer{os.Stdout}
	fp, _ := os.OpenFile(fn, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	writers = append(writers, fp)
	logger := NewLogger("test", io.MultiWriter(writers...), DEBUG, log.LstdFlags|log.Lshortfile)
	logger.Debug("Hello world")
	logger.Error("System Halt!")

}

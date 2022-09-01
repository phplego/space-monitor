package main

import (
	"fmt"
	"github.com/fatih/color"
	"io"
	"os"
	"testing"
)

func TestPipe1(t *testing.T) {
	reportFile, _ := os.OpenFile("out.txt", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	origStdout := os.Stdout
	multiWriter := io.MultiWriter(reportFile, origStdout)
	pipeReader, pipeWriter, _ := os.Pipe()
	os.Stdout = pipeWriter
	//color.Output = os.Stdout
	go func() {
		for {
			io.Copy(multiWriter, pipeReader) // stucks forever
		}
	}()

	//multiWriter.Write([]byte(" "))

	fmt.Println("12312312312312312312313")
	color.HiRed("Hello!")

	reportFile.Close()

	os.Stdout = origStdout
	bytes, _ := os.ReadFile("out.txt")
	fmt.Println("FILE CONTETN:", string(bytes))
}

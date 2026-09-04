package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestReadControllerSecretFromPipeKeepsOneLineContract(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer reader.Close()
	if _, err := io.WriteString(writer, "synthetic-pipe-secret\nsecond-line\n"); err != nil {
		t.Fatalf("failed to write pipe input: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}

	var prompt bytes.Buffer
	value, err := readControllerSecret(reader, &prompt)
	if err != nil {
		t.Fatalf("pipe secret read failed: %v", err)
	}
	if value != "synthetic-pipe-secret" {
		t.Fatal("pipe secret reader did not preserve the first input line")
	}
	if prompt.Len() != 0 {
		t.Fatal("pipe secret reader unexpectedly wrote an interactive prompt")
	}
}

func TestReadControllerSecretFromPipePreservesBlankForCommandValidation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer reader.Close()
	if _, err := io.WriteString(writer, "   \n"); err != nil {
		t.Fatalf("failed to write blank pipe input: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}

	value, err := readControllerSecret(reader, nil)
	if err != nil {
		t.Fatalf("blank line should be read before command-level validation: %v", err)
	}
	if value != "   " {
		t.Fatal("pipe reader changed the blank input")
	}
}

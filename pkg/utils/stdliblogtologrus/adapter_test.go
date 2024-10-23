package stdliblogtologrus

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/flant/shell-operator/pkg/unilogger"
)

type testLogLine struct {
	Level   string `json:"level"`
	Message string `json:"msg"`
	Logger  string `json:"logger"`
}

func TestStdlibLogAdapter(t *testing.T) {
	t.Run("Simple", func(t *testing.T) {
		buf := bytes.Buffer{}
		logger := unilogger.NewLogger(unilogger.Options{})

		logger.SetOutput(&buf)

		InitAdapter(logger)

		log.Print("test string for a check")
		log.Print("another string")

		scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))

		scanner.Scan()
		assertLogLine(t, scanner.Text(), "test string for a check")

		scanner.Scan()
		assertLogLine(t, scanner.Text(), "another string")
	})
}

func assertLogLine(t *testing.T, line string, expected string) {
	logLine := testLogLine{}

	fmt.Println(line)
	err := json.Unmarshal([]byte(line), &logLine)
	require.NoError(t, err)
	require.Equal(t, "helm", logLine.Logger)
	require.Equal(t, "info", logLine.Level)
	require.Contains(t, logLine.Message, expected)
}

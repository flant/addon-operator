package stdliblogtologrus

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type testLogLine struct {
	Level   string `json:"level"`
	Message string `json:"msg"`
	Source  string `json:"source"`
}

func TestStdlibLogAdapter(t *testing.T) {
	t.Run("Simple", func(t *testing.T) {
		InitAdapter()

		buf := bytes.Buffer{}
		logrus.SetOutput(&buf)
		logrus.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})

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

	err := json.Unmarshal([]byte(line), &logLine)
	require.NoError(t, err)
	require.Equal(t, "helm", logLine.Source)
	require.Equal(t, "info", logLine.Level)
	require.Contains(t, logLine.Message, expected)
}

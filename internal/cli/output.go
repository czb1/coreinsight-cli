package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

const (
	codeInvalidParams = -32602
	codeInternalError = -32603
	codeBackendError  = -32000
)

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
	ID      string      `json:"id"`
}

func requestID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "req-0"
	}
	return "req-" + hex.EncodeToString(b)
}

func emit(v rpcResponse) {
	v.JSONRPC = "2.0"
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func success(id string, result interface{}) { emit(rpcResponse{ID: id, Result: result}) }
func failure(id string, code int, message string, data interface{}) {
	emit(rpcResponse{ID: id, Error: &rpcError{Code: code, Message: message, Data: data}})
}

func plainHelp(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, format, args...)
}

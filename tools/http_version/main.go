package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	addr := "http://127.0.0.1:10001/api/v1/version"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	fmt.Printf("POST %s\n", addr)
	body, _ := json.Marshal(map[string]string{})
	resp, err := http.Post(addr, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	fmt.Printf("status: %d\n", resp.StatusCode)
	fmt.Printf("body:   %s\n", string(data))
}

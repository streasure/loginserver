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
	addr := "http://127.0.0.1:10001/api/v1/login"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

 openId := "test-user"
 ptId := int32(1)
	if len(os.Args) > 2 {
		openId = os.Args[2]
	}
	if len(os.Args) > 3 {
		fmt.Sscanf(os.Args[3], "%d", &ptId)
	}

	fmt.Printf("POST %s  openId=%s  ptId=%d\n", addr, openId, ptId)
	body, _ := json.Marshal(map[string]any{
		"openId": openId,
		"ptid":   ptId,
	})
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

// bandidrill 封禁收敛 P1 drill 客户端：
//
//	login  <sgateAddr> <serverId> <accountId> <loginToken>
//	  连接 sgate → LoginGate → 打印 LOGIN_OK/LOGIN_REJECT → 阻塞等待踢下线
//	  （CMD_KICK_NOTIFY 或连接断开），打印 KICKED/CLOSED 后退出。
//
//	relogin <sgateAddr> <serverId> <accountId> <loginToken>
//	  单次 LoginGate 尝试，打印 LOGIN_OK/LOGIN_REJECT 后立即退出（测封禁拦截）。
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	protoGw "github.com/streasure/protocol/gateway"
	protoLogic "github.com/streasure/protocol/logic"
	"google.golang.org/protobuf/proto"
)

const (
	cmdLoginGate    int32 = 1000001
	cmdLoginGateAck int32 = 1000002
	cmdKickNotify   int32 = 1100009
)

func main() {
	if len(os.Args) < 6 {
		fmt.Fprintf(os.Stderr, "usage: %s login|relogin <sgateAddr> <serverId> <accountId> <loginToken>\n", os.Args[0])
		os.Exit(2)
	}
	mode, addr, serverID, accountID, token := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5]

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		fmt.Printf("DIAL_FAIL %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	body, _ := proto.Marshal(&protoGw.LoginGateReq{
		ServerId: serverID,
		UserId:   accountID,
		LoginKey: token,
	})
	if err := sendFrame(conn, &protoGw.MessageFrame{Cmd: cmdLoginGate, SeqId: 1, Body: body}); err != nil {
		fmt.Printf("SEND_FAIL %v\n", err)
		os.Exit(1)
	}

	frame, err := readFrame(conn, 5*time.Second)
	if err != nil {
		fmt.Printf("ACK_TIMEOUT %v\n", err)
		os.Exit(1)
	}
	if frame.Cmd != cmdLoginGateAck {
		fmt.Printf("UNEXPECTED_CMD %d\n", frame.Cmd)
		os.Exit(1)
	}
	ack := &protoGw.LoginGateAck{}
	if err := proto.Unmarshal(frame.Body, ack); err != nil {
		fmt.Printf("ACK_DECODE_FAIL %v\n", err)
		os.Exit(1)
	}
	if ack.Code != 0 {
		fmt.Printf("LOGIN_REJECT code=%d message=%s\n", ack.Code, ack.Message)
		os.Exit(3)
	}
	fmt.Printf("LOGIN_OK accountId=%s conn=%s\n", accountID, ack.SessionId)

	if mode == "relogin" {
		os.Exit(0)
	}

	// login 模式：阻塞等待踢下线（KICK_NOTIFY 帧或连接关闭）
	for {
		frame, err := readFrame(conn, 60*time.Second)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				fmt.Printf("WAIT_TIMEOUT no kick within 60s\n")
				os.Exit(4)
			}
			fmt.Printf("CLOSED err=%v\n", err)
			os.Exit(0)
		}
		if frame.Cmd == cmdKickNotify {
			n := &protoLogic.KickNotify{}
			if err := proto.Unmarshal(frame.Body, n); err == nil {
				fmt.Printf("KICKED reason=%s code=%d message=%s\n", n.Reason, n.Code, n.Message)
			} else {
				fmt.Printf("KICKED (decode fail)\n")
			}
			os.Exit(0)
		}
	}
}

func sendFrame(conn net.Conn, frame *protoGw.MessageFrame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(data)))
	_, err = conn.Write(append(header, data...))
	return err
}

func readFrame(conn net.Conn, timeout time.Duration) (*protoGw.MessageFrame, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	dataLen := binary.BigEndian.Uint32(header)
	if dataLen == 0 || dataLen > 4*1024*1024 {
		return nil, fmt.Errorf("invalid frame length %d", dataLen)
	}
	data := make([]byte, dataLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}
	frame := &protoGw.MessageFrame{}
	if err := proto.Unmarshal(data, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

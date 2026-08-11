// Package rcon 实现 Source 引擎 RCON 协议客户端。
//
// 协议参考: https://developer.valvesoftware.com/wiki/Source_RCON_Protocol
package rcon

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	typeAuth         = 3
	typeAuthResponse = 2
	typeExecCommand  = 2
	typeResponse     = 0
)

// ErrAuthFailed 认证失败。
var ErrAuthFailed = errors.New("rcon: 认证失败")

var bufPool = sync.Pool{New: func() interface{} { return new(bytes.Buffer) }}

// Client RCON 客户端。
type Client struct {
	conn    net.Conn
	timeout time.Duration
}

// Dial 连接 RCON 服务器。
func Dial(addr string, password string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("rcon 连接失败: %w", err)
	}

	c := &Client{conn: conn, timeout: timeout}
	if err := c.auth(password); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// Close 关闭连接。
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) auth(password string) error {
	if err := c.writePacket(typeAuth, password); err != nil {
		return err
	}
	// 服务器可能先回一个空 SERVERDATA_RESPONSE_VALUE 再回 AUTH_RESPONSE
	// 最多尝试 8 次（正常情况 1-2 次即可）
	for i := 0; i < 8; i++ {
		pktType, _, err := c.readPacket()
		if err != nil {
			return err
		}
		if pktType == typeAuthResponse {
			break
		}
	}
	return nil
}

// Execute 执行命令并返回响应文本。
func (c *Client) Execute(command string) (string, error) {
	if err := c.writePacket(typeExecCommand, command); err != nil {
		return "", err
	}
	pktType, body, err := c.readPacket()
	if err != nil {
		return "", err
	}
	if pktType != typeResponse {
		return "", fmt.Errorf("rcon: 意外的响应类型 %d", pktType)
	}
	return string(bytes.TrimRight(body, "\x00")), nil
}

// --- 协议底层 ---

type packetHeader struct {
	Size int32
	ID   int32
	Type int32
}

func (c *Client) writePacket(pktType int32, body string) error {
	c.conn.SetDeadline(time.Now().Add(c.timeout))

	bodyBytes := []byte(body)
	// body + 2 个 null 终止符
	size := int32(len(bodyBytes) + 10) // 4(ID)+4(Type)+body+1+1

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := binary.Write(buf, binary.LittleEndian, size); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.LittleEndian, int32(1)); err != nil { // ID
		return err
	}
	if err := binary.Write(buf, binary.LittleEndian, pktType); err != nil {
		return err
	}
	buf.Write(bodyBytes)
	buf.WriteByte(0) // body 终止
	buf.WriteByte(0) // 包尾

	// 拷贝字节后再归还池，防止竞态
	pkt := append([]byte(nil), buf.Bytes()...)
	bufPool.Put(buf)

	_, err := c.conn.Write(pkt)
	return err
}

func (c *Client) readPacket() (pktType int32, body []byte, err error) {
	c.conn.SetDeadline(time.Now().Add(c.timeout))

	var hdr packetHeader
	if err := binary.Read(c.conn, binary.LittleEndian, &hdr.Size); err != nil {
		return 0, nil, fmt.Errorf("读取 Size: %w", err)
	}
	if hdr.Size < 10 || hdr.Size > 4096 {
		return 0, nil, fmt.Errorf("rcon: 无效包大小 %d", hdr.Size)
	}
	if err := binary.Read(c.conn, binary.LittleEndian, &hdr.ID); err != nil {
		return 0, nil, fmt.Errorf("读取 ID: %w", err)
	}
	if err := binary.Read(c.conn, binary.LittleEndian, &hdr.Type); err != nil {
		return 0, nil, fmt.Errorf("读取 Type: %w", err)
	}
	// 剩余 = Size - 8 (ID+Type) ，包含 body + 2 null
	remaining := int(hdr.Size) - 8
	bodyBuf := make([]byte, remaining)
	if _, err := io.ReadFull(c.conn, bodyBuf); err != nil {
		return 0, nil, fmt.Errorf("读取 body: %w", err)
	}
	// 去掉末尾两个 null
	if len(bodyBuf) >= 2 {
		bodyBuf = bodyBuf[:len(bodyBuf)-2]
	}
	// ID == -1 表示认证失败
	if hdr.ID == -1 {
		return 0, nil, ErrAuthFailed
	}
	return hdr.Type, bodyBuf, nil
}

// ExecuteOnce 建立→认证→执行→关闭的一次性调用。
// ctx 用于取消请求。适合低频指令场景，避免长连接占用资源。
func ExecuteOnce(addr string, password string, command string, timeout time.Duration, ctx context.Context) (string, error) {
	c, err := Dial(addr, password, timeout)
	if err != nil {
		return "", err
	}
	defer c.Close()

	// 在 goroutine 中执行，监听 ctx 取消
	type result struct {
		reply string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		reply, err := c.Execute(command)
		done <- result{reply, err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		return r.reply, r.err
	}
}

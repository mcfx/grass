package grass

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"time"
)

const PacketMagicNumber uint8 = 114514 % 256

const (
	GrassPacketIPv4    uint8 = 0
	GrassPacketPing    uint8 = 1
	GrassPacketInfo    uint8 = 2
	GrassPacketPong    uint8 = 3
	GrassPacketClients uint8 = 4
	GrassPacketFarPing uint8 = 5
	GrassPacketExpired uint8 = 255
)

type Packet struct {
	TmpKey    uint32
	Timestamp uint16
	Type      uint8
	CRC32     uint32
	Length    uint32
	Content   []byte
}

type QueuedPacket struct {
	Type uint8
	Data []byte
}

func pkcs5Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

func pkcs5UnPadding(origData []byte, blockSize int) ([]byte, error) {
	length := len(origData)
	unpadding := int(origData[length-1])
	if unpadding == 0 || unpadding > blockSize {
		return nil, fmt.Errorf("Invalid padding")
	}
	for i := length - unpadding; i < length; i++ {
		if int(origData[i]) != unpadding {
			return nil, fmt.Errorf("Invalid padding")
		}
	}
	return origData[:length-unpadding], nil
}

func decodePacket(r *Packet, s io.Reader, sharedKey []byte) error {
	buf := make([]byte, 4)
	_, err := io.ReadFull(s, buf)
	if err != nil {
		return err
	}
	r.TmpKey = binary.LittleEndian.Uint32(buf)
	tmpBlock, _ := aes.NewCipher(sharedKey)
	buffer := bytes.NewBuffer(nil)
	binary.Write(buffer, binary.LittleEndian, r.TmpKey)
	binary.Write(buffer, binary.LittleEndian, r.TmpKey)
	binary.Write(buffer, binary.LittleEndian, r.TmpKey)
	binary.Write(buffer, binary.LittleEndian, r.TmpKey)
	key := buffer.Bytes()
	tmpBlock.Decrypt(key, key)

	buf = make([]byte, 16)
	_, err = io.ReadFull(s, buf)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	cryptor := cipher.NewCBCDecrypter(block, key)
	cryptor.CryptBlocks(buf, buf)

	if uint8(buf[0]) != PacketMagicNumber {
		return fmt.Errorf("Packet magic number mismatch")
	}
	r.Timestamp = binary.LittleEndian.Uint16(buf[1:3])
	r.Type = uint8(buf[3])
	r.CRC32 = binary.LittleEndian.Uint32(buf[4:8])
	r.Length = binary.LittleEndian.Uint32(buf[8:12])
	buf[4] = 0
	buf[5] = 0
	buf[6] = 0
	buf[7] = 0
	hash := crc32.NewIEEE()
	hash.Write(buf)

	buf = make([]byte, r.Length)
	n, err := io.ReadFull(s, buf)
	if err != nil {
		return err
	}
	nt := n - n%16
	cryptor.CryptBlocks(buf[:nt], buf[:nt])
	hash.Write(buf)
	sum := hash.Sum32()
	if sum != r.CRC32 {
		return fmt.Errorf("CRC32 mismatch")
	}

	r.Content, err = pkcs5UnPadding(buf[:nt], 16)
	if err != nil {
		return err
	}

	return nil
}

func encodePacket(r *Packet, s Writer, sharedKey []byte) error {
	tmpBlock, _ := aes.NewCipher(sharedKey)
	buffer := bytes.NewBuffer(nil)
	binary.Write(buffer, binary.LittleEndian, r.TmpKey)
	binary.Write(buffer, binary.LittleEndian, r.TmpKey)
	binary.Write(buffer, binary.LittleEndian, r.TmpKey)
	binary.Write(buffer, binary.LittleEndian, r.TmpKey)
	key := buffer.Bytes()
	tmpBlock.Decrypt(key, key)

	r.Timestamp = uint16(time.Now().Unix() % 65536)
	r.CRC32 = 0

	paddedContent := pkcs5Padding(r.Content, 16)
	nadd := rand.Intn(16)
	r.Length = uint32(len(paddedContent) + nadd)

	buffer = bytes.NewBuffer(nil)
	binary.Write(buffer, binary.LittleEndian, PacketMagicNumber)
	binary.Write(buffer, binary.LittleEndian, r.Timestamp)
	binary.Write(buffer, binary.LittleEndian, r.Type)
	binary.Write(buffer, binary.LittleEndian, r.CRC32)
	binary.Write(buffer, binary.LittleEndian, r.Length)
	buffer.Write([]byte{0, 0, 0, 0})
	buf := buffer.Bytes()

	buf = append(buf, paddedContent...)

	tmp := make([]byte, nadd)
	rand.Read(tmp)
	buf = append(buf, tmp...)

	r.CRC32 = crc32.ChecksumIEEE(buf)
	binary.LittleEndian.PutUint32(buf[4:8], r.CRC32)

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	cryptor := cipher.NewCBCEncrypter(block, key)
	nt := 16 + uint(r.Length-r.Length%16)
	cryptor.CryptBlocks(buf[:nt], buf[:nt])

	buf2 := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf2, r.TmpKey)
	s.M.Lock()
	_, err = s.W.Write(buf2)
	if err != nil {
		s.M.Unlock()
		return err
	}
	_, err = s.W.Write(buf)
	if err != nil {
		s.M.Unlock()
		return err
	}
	err = s.W.Flush()
	if err != nil {
		s.M.Unlock()
		return err
	}
	s.M.Unlock()
	return nil
}

func ReadPacket(pkt *Packet, conn *Conn, client *Client) error {
	if err := decodePacket(pkt, conn.Reader, client.Key); err != nil {
		return err
	}
	if conn.peerTmpKeys.HasKey(pkt.TmpKey) {
		pkt.Type = GrassPacketExpired
		return nil
	}
	a := time.Now().Unix() % 65536
	b := int64(pkt.Timestamp)
	if b-a < 120 && a-b < 120 {
		conn.peerTmpKeys.AddKey(pkt.TmpKey)
		return nil
	}
	if b+65536-a < 120 || a+65536-b < 120 {
		conn.peerTmpKeys.AddKey(pkt.TmpKey)
		return nil
	}
	pkt.Type = GrassPacketExpired
	return nil
}

func WritePacket(pkt *Packet, conn *Conn, client *Client) error {
	for {
		pkt.TmpKey = rand.Uint32()
		if !conn.tmpKeys.HasKey(pkt.TmpKey) {
			break
		}
	}
	conn.tmpKeys.AddKey(pkt.TmpKey)
	if err := encodePacket(pkt, conn.Writer, client.Key); err != nil {
		return err
	}
	return nil
}

func WriteRawPacket(typ uint8, content []byte, conn *Conn, client *Client) error {
	var pkt Packet
	pkt.Type = typ
	pkt.Content = content
	return WritePacket(&pkt, conn, client)
}

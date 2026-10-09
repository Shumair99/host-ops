package storage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"sync"
	"time"
)

// Blocks are independent: losing one never makes the next undecodable. JSON
// byte deltas preserve numbers, omitted fields and nulls exactly as written.
const (
	blockSamples     = 512
	blockTargetBytes = 256 * 1024
	maxBlockBytes    = 16 * 1024 * 1024
)

type observation struct {
	id   int64
	at   time.Time
	data []byte
}

func appendFrame(dst []byte, previous observation, next observation) []byte {
	dst = binary.AppendVarint(dst, next.id-previous.id)
	dst = binary.AppendVarint(dst, next.at.Unix()-previous.at.Unix())
	dst = binary.AppendVarint(dst, int64(next.at.Nanosecond()-previous.at.Nanosecond()))
	header := len(dst)
	dst = append(dst, 0)
	prefix := 0
	for prefix < len(previous.data) && prefix < len(next.data) && previous.data[prefix] == next.data[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(previous.data)-prefix && suffix < len(next.data)-prefix && previous.data[len(previous.data)-1-suffix] == next.data[len(next.data)-1-suffix] {
		suffix++
	}
	middle := next.data[prefix : len(next.data)-suffix]
	dst = binary.AppendUvarint(dst, uint64(prefix))
	dst = binary.AppendUvarint(dst, uint64(suffix))
	dst = binary.AppendUvarint(dst, uint64(len(middle)))
	dst = append(dst, middle...)
	// Token edits handle separated timestamp/latency/value changes without
	// re-storing the unchanged JSON between them. Choose the smaller delta.
	if patches := tokenEdits(previous.data, next.data, next.at); len(patches) > 0 && len(patches) < len(dst)-header {
		dst = append(dst[:header], patches...)
	}
	return dst
}

type tokenSpan struct{ start, end int }

func jsonTokens(data []byte) []tokenSpan {
	if len(data) > 64*1024 {
		return nil
	}
	spans := []tokenSpan{}
	for i := 0; i < len(data); {
		start := i
		switch data[i] {
		case '"':
			i++
			for i < len(data) {
				if data[i] == '\\' {
					i += 2
					continue
				}
				if data[i] == '"' {
					i++
					break
				}
				i++
			}
		case '{', '}', ':', ',', '[', ']':
			i++
		default:
			i++
			for i < len(data) && !bytes.ContainsRune([]byte("{}:,[]\""), rune(data[i])) {
				i++
			}
		}
		if i > len(data) {
			return nil
		}
		spans = append(spans, tokenSpan{start, i})
		if len(spans) > 4096 {
			return nil
		}
	}
	return spans
}

func tokenEdits(previous, next []byte, at time.Time) []byte {
	if len(previous) == 0 {
		return nil
	}
	a, b := jsonTokens(previous), jsonTokens(next)
	if len(a) == 0 || len(a) != len(b) {
		return nil
	}
	changes := 0
	var edits []byte
	last := -1
	stamp, _ := at.UTC().MarshalJSON()
	for i := range a {
		value := next[b[i].start:b[i].end]
		if bytes.Equal(previous[a[i].start:a[i].end], value) {
			continue
		}
		edits = binary.AppendUvarint(edits, uint64(i-last))
		edits = appendToken(edits, previous[a[i].start:a[i].end], value, stamp)
		last = i
		changes++
	}
	out := []byte{1}
	out = binary.AppendUvarint(out, uint64(changes))
	return append(out, edits...)
}

// Canonical numeric lexemes can be encoded as integer deltas or exact IEEE
// float XORs. Other spellings (1.2300, exponent style, etc.) stay literal.
func appendToken(dst, previous, value, stamp []byte) []byte {
	if bytes.Equal(value, stamp) {
		return binary.AppendUvarint(dst, 1)
	}
	if len(value) > 0 && (value[0] == '-' || value[0] >= '0' && value[0] <= '9') {
		if n, err := strconv.ParseInt(string(value), 10, 64); err == nil && string(value) == strconv.FormatInt(n, 10) {
			p, _ := strconv.ParseInt(string(previous), 10, 64)
			if !(p > 0 && n < math.MinInt64+p || p < 0 && n > math.MaxInt64+p) {
				delta := binary.AppendVarint(nil, n-p)
				if len(delta) < len(value) {
					dst = binary.AppendUvarint(dst, 2)
					return append(dst, delta...)
				}
			}
		}
		if n, err := strconv.ParseFloat(string(value), 64); err == nil {
			canonical, _ := json.Marshal(n)
			if bytes.Equal(canonical, value) {
				p, _ := strconv.ParseFloat(string(previous), 64)
				xor := binary.AppendUvarint(nil, math.Float64bits(n)^math.Float64bits(p))
				if len(xor) < len(value) {
					dst = binary.AppendUvarint(dst, 3)
					return append(dst, xor...)
				}
			}
		}
	}
	dst = binary.AppendUvarint(dst, uint64(len(value))<<2)
	return append(dst, value...)
}

func readToken(reader *bytes.Reader, previous []byte, at time.Time) ([]byte, error) {
	tag, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, err
	}
	switch tag & 3 {
	case 1:
		if tag != 1 {
			return nil, fmt.Errorf("invalid history timestamp tag")
		}
		return at.UTC().MarshalJSON()
	case 2:
		if tag != 2 {
			return nil, fmt.Errorf("invalid history integer tag")
		}
		delta, err := binary.ReadVarint(reader)
		if err != nil {
			return nil, err
		}
		p, _ := strconv.ParseInt(string(previous), 10, 64)
		if delta > 0 && p > math.MaxInt64-delta || delta < 0 && p < math.MinInt64-delta {
			return nil, fmt.Errorf("history integer overflow")
		}
		return strconv.AppendInt(nil, p+delta, 10), nil
	case 3:
		if tag != 3 {
			return nil, fmt.Errorf("invalid history float tag")
		}
		xor, err := binary.ReadUvarint(reader)
		if err != nil {
			return nil, err
		}
		p, _ := strconv.ParseFloat(string(previous), 64)
		return json.Marshal(math.Float64frombits(math.Float64bits(p) ^ xor))
	default:
		length := tag >> 2
		if length > uint64(reader.Len()) || length > maxBlockBytes {
			return nil, fmt.Errorf("invalid history literal length")
		}
		value := make([]byte, int(length))
		_, err = io.ReadFull(reader, value)
		return value, err
	}
}

func readPayload(reader *bytes.Reader, previous []byte, at time.Time) ([]byte, error) {
	mode, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if mode == 1 {
		spans := jsonTokens(previous)
		count, err := binary.ReadUvarint(reader)
		if err != nil || len(spans) == 0 || count > uint64(len(spans)) {
			return nil, fmt.Errorf("invalid history token count")
		}
		out := make([]byte, 0, len(previous))
		start, last := 0, -1
		for i := uint64(0); i < count; i++ {
			delta, err := binary.ReadUvarint(reader)
			if err != nil || delta == 0 || delta > uint64(len(spans)) {
				return nil, fmt.Errorf("invalid history token index")
			}
			index := last + int(delta)
			if index >= len(spans) {
				return nil, fmt.Errorf("invalid history token index")
			}
			value, err := readToken(reader, previous[spans[index].start:spans[index].end], at)
			if err != nil || len(out)+spans[index].start-start+len(value) > maxBlockBytes {
				return nil, fmt.Errorf("invalid history token length")
			}
			out = append(out, previous[start:spans[index].start]...)
			out = append(out, value...)
			start, last = spans[index].end, index
		}
		if len(out)+len(previous)-start > maxBlockBytes {
			return nil, fmt.Errorf("history payload exceeds decode limit")
		}
		return append(out, previous[start:]...), nil
	}
	if mode != 0 {
		return nil, fmt.Errorf("unsupported history delta mode")
	}
	prefix, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, fmt.Errorf("corrupt history prefix")
	}
	suffix, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, fmt.Errorf("corrupt history suffix")
	}
	length, err := binary.ReadUvarint(reader)
	if err != nil || prefix > uint64(len(previous)) || suffix > uint64(len(previous))-prefix || length > uint64(reader.Len()) || prefix+suffix+length > maxBlockBytes {
		return nil, fmt.Errorf("corrupt history byte delta")
	}
	payload := make([]byte, int(prefix+length+suffix))
	copy(payload, previous[:prefix])
	if _, err = io.ReadFull(reader, payload[prefix:prefix+length]); err != nil {
		return nil, err
	}
	copy(payload[prefix+length:], previous[uint64(len(previous))-suffix:])
	return payload, nil
}

func encodeFrames(samples []observation) []byte {
	var data []byte
	previous := observation{at: time.Unix(0, 0)}
	for _, sample := range samples {
		data = appendFrame(data, previous, sample)
		previous = sample
	}
	return data
}

type blockPacker struct {
	buffer bytes.Buffer
	writer *zlib.Writer
}

func packerPool(level int) *sync.Pool {
	return &sync.Pool{New: func() any { p := &blockPacker{}; p.writer, _ = zlib.NewWriterLevel(&p.buffer, level); return p }}
}

var packers = packerPool(zlib.BestSpeed)
var sealedPackers = packerPool(zlib.BestCompression)

func packBlock(raw []byte) ([]byte, error) {
	return packWithPool(raw, packers)
}

func packSealedBlock(raw []byte) ([]byte, error) { return packWithPool(raw, sealedPackers) }

func packWithPool(raw []byte, pool *sync.Pool) ([]byte, error) {
	p := pool.Get().(*blockPacker)
	defer pool.Put(p)
	p.buffer.Reset()
	p.writer.Reset(&p.buffer)
	if _, err := p.writer.Write(raw); err != nil {
		return nil, err
	}
	if err := p.writer.Close(); err != nil {
		return nil, err
	}
	return bytes.Clone(p.buffer.Bytes()), nil
}

type blockUnpacker struct {
	input  bytes.Reader
	reader io.ReadCloser
}

var unpackers sync.Pool

func unpackBlock(data []byte, encoding int) ([]byte, error) {
	if encoding == 0 {
		if len(data) > maxBlockBytes {
			return nil, fmt.Errorf("history block exceeds decode limit")
		}
		return data, nil
	}
	if encoding != 1 && encoding != 2 {
		return nil, fmt.Errorf("unsupported history block encoding %d", encoding)
	}
	p, _ := unpackers.Get().(*blockUnpacker)
	if p == nil {
		p = &blockUnpacker{}
	}
	p.input.Reset(data)
	var err error
	if p.reader == nil {
		p.reader, err = zlib.NewReader(&p.input)
	} else {
		err = p.reader.(zlib.Resetter).Reset(&p.input, nil)
	}
	if err != nil {
		if p.reader != nil {
			p.reader.Close()
		}
		p.input.Reset(nil)
		return nil, fmt.Errorf("corrupt history block: %w", err)
	}
	healthy := false
	defer func() {
		p.reader.Close()
		p.input.Reset(nil)
		if healthy {
			unpackers.Put(p)
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(p.reader, maxBlockBytes+1))
	if err != nil {
		return nil, fmt.Errorf("corrupt history block: %w", err)
	}
	if len(raw) > maxBlockBytes {
		return nil, fmt.Errorf("history block exceeds decode limit")
	}
	healthy = true
	return raw, nil
}

func decodeFrames(data []byte, count int) ([]observation, error) {
	if count < 1 || count > blockSamples || len(data) > maxBlockBytes {
		return nil, fmt.Errorf("invalid history block bounds")
	}
	reader := bytes.NewReader(data)
	previous := observation{at: time.Unix(0, 0)}
	out := make([]observation, 0, count)
	totalBytes := 0
	for i := 0; i < count; i++ {
		idDelta, err := binary.ReadVarint(reader)
		if err != nil {
			return nil, fmt.Errorf("corrupt history record id: %w", err)
		}
		seconds, err := binary.ReadVarint(reader)
		if err != nil {
			return nil, fmt.Errorf("corrupt history timestamp: %w", err)
		}
		nanoDelta, err := binary.ReadVarint(reader)
		if err != nil || nanoDelta < -999999999 || nanoDelta > 999999999 {
			return nil, fmt.Errorf("corrupt history nanosecond delta")
		}
		nanos := int64(previous.at.Nanosecond()) + nanoDelta
		if nanos < 0 || nanos >= 1_000_000_000 {
			return nil, fmt.Errorf("corrupt history nanoseconds")
		}
		if idDelta <= 0 || previous.id > int64(^uint64(0)>>1)-idDelta {
			return nil, fmt.Errorf("invalid history id order")
		}
		atSeconds := previous.at.Unix() + seconds
		if (seconds > 0 && atSeconds < previous.at.Unix()) || (seconds < 0 && atSeconds > previous.at.Unix()) {
			return nil, fmt.Errorf("invalid history timestamp delta")
		}
		at := time.Unix(atSeconds, nanos).UTC()
		if at.Year() < 1 || at.Year() > 9999 {
			return nil, fmt.Errorf("history timestamp is outside RFC3339 range")
		}
		payload, err := readPayload(reader, previous.data, at)
		if err != nil {
			return nil, err
		}
		if !json.Valid(payload) {
			return nil, fmt.Errorf("corrupt history JSON")
		}
		totalBytes += len(payload)
		if totalBytes > maxBlockBytes {
			return nil, fmt.Errorf("history block exceeds reconstructed byte limit")
		}
		previous = observation{id: previous.id + idDelta, at: at, data: payload}
		out = append(out, previous)
	}
	if reader.Len() != 0 {
		return nil, fmt.Errorf("trailing history block bytes")
	}
	return out, nil
}

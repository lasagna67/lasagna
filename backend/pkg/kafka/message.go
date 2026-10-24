package kafka

import (
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Message — сообщение Kafka без привязки к franz-go, чтобы обёртки не зависели от клиента.
type Message struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers []Header

	// Заполняются при чтении, при отправке игнорируются:
	Partition int32
	Offset    int64
	Timestamp time.Time
}

// Header — заголовок сообщения. Слайс, а не map: ключи в Kafka могут повторяться.
type Header struct {
	Key   string
	Value []byte
}

func toRecord(msg Message) *kgo.Record {
	rec := &kgo.Record{
		Topic: msg.Topic,
		Key:   msg.Key,
		Value: msg.Value,
	}
	for _, h := range msg.Headers {
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: h.Key, Value: h.Value})
	}
	return rec
}

func fromRecord(rec *kgo.Record) Message {
	msg := Message{
		Topic:     rec.Topic,
		Key:       rec.Key,
		Value:     rec.Value,
		Partition: rec.Partition,
		Offset:    rec.Offset,
		Timestamp: rec.Timestamp,
	}
	for _, h := range rec.Headers {
		msg.Headers = append(msg.Headers, Header{Key: h.Key, Value: h.Value})
	}
	return msg
}

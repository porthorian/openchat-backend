package chat

import (
	"errors"
	"testing"
)

func TestBulkReadAcksAreAtomicAndMonotonic(t *testing.T) {
	svc := NewService("http://localhost:8080")
	result, err := svc.UpdateReadAcksBulk(SeedServerIDHarbor, "uid_test", []BulkReadAckInput{{ChannelID: "ch_general", LastReadMessageID: "msg_seed_02"}, {ChannelID: "ch_design", LastReadMessageID: "msg_seed_11"}})
	if err != nil || len(result) != 2 || result[0].LastReadMessageID != "msg_seed_02" {
		t.Fatalf("bulk read failed: %v %+v", err, result)
	}
	_, err = svc.UpdateReadAcksBulk(SeedServerIDHarbor, "uid_test", []BulkReadAckInput{{ChannelID: "ch_general", LastReadMessageID: "msg_seed_01"}, {ChannelID: "ch_design", LastReadMessageID: "missing"}})
	if !errors.Is(err, ErrReadAckMessageNotFound) {
		t.Fatalf("invalid batch accepted: %v", err)
	}
	current, err := svc.GetReadAck("ch_general", "uid_test")
	if err != nil || current.LastReadMessageID != "msg_seed_02" {
		t.Fatalf("batch partially applied: %v %+v", err, current)
	}
	result, err = svc.UpdateReadAcksBulk(SeedServerIDHarbor, "uid_test", []BulkReadAckInput{{ChannelID: "ch_general", LastReadMessageID: "msg_seed_01"}})
	if err != nil || result[0].LastReadMessageID != "msg_seed_02" {
		t.Fatalf("cursor moved backward: %v %+v", err, result)
	}
	_, err = svc.UpdateReadAcksBulk(SeedServerIDHarbor, "uid_test", []BulkReadAckInput{{ChannelID: "tl_ch_general"}})
	if !errors.Is(err, ErrBulkReadAckInvalid) {
		t.Fatalf("cross-server ack accepted: %v", err)
	}
}

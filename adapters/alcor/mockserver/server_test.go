package mockserver

import (
	"encoding/json"
	"strings"
	"testing"
)

// 这个测试只验证 createInput 能区分「字段缺席」和「传了空串」。
//
// 之所以要单独测：真正的用例是通过 HTTP 走的，一旦这里判断错了，
// 表现是「空池被当成自动选池」，看起来像是自动选池正常工作了。
func TestCreateInputDistinguishesAbsentFromEmptyPoolID(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantPresent   bool
		wantPoolID    string
	}{
		{name: "字段缺席", body: `{"owner_type":"run_attempt","lease_seconds":600}`, wantPresent: false, wantPoolID: ""},
		{name: "空串", body: `{"pool_id":"","owner_type":"run_attempt","lease_seconds":600}`, wantPresent: true, wantPoolID: ""},
		{name: "正常池", body: `{"pool_id":"pool_000000000000001","owner_type":"run_attempt","lease_seconds":600}`, wantPresent: true, wantPoolID: "pool_000000000000001"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoder := json.NewDecoder(strings.NewReader(test.body))
			decoder.DisallowUnknownFields()
			var input createInput
			if err := decoder.Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.poolIDPresent != test.wantPresent {
				t.Fatalf("poolIDPresent=%v want=%v", input.poolIDPresent, test.wantPresent)
			}
			if input.PoolID != test.wantPoolID {
				t.Fatalf("PoolID=%q want=%q", input.PoolID, test.wantPoolID)
			}
		})
	}
}
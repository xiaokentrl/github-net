package hosts

import (
	"testing"
)

// 标记匹配泛化（2026-11-15 改名支持）：id 段接受任意值——产品改名后，旧版写进
// /etc/hosts 的区段（旧 id 标记）仍能被认出并正常还原；摘要只算映射行不含标记行，
// 所以「读旧标记的账」与「写新标记的盘」账实零变化。
func TestParseAcceptsAnyMarkerID(t *testing.T) {
	content := `# BEGIN oldname managed hosts v1
127.0.0.1 old.example.com
# END oldname managed hosts v1
`
	block := Parse(content)
	if !block.Present || !block.Usable() {
		t.Fatalf("旧 id 标记的区段该被认出且可用，实际 %+v", block)
	}
	if len(block.Records) != 1 || block.Records[0].IP != "127.0.0.1" {
		t.Errorf("区段内的映射该照常解析，实际 %+v", block.Records)
	}
}

package github

// All 按优先序给出三份源：默认源在前，两份备源在后。
// 顺序就是「全部」方法合并时先到先得的顺序，也是各源方法在界面上的自然次序。
func All() []Source {
	return []Source{GitCDN, HelloGitHub, GitLab}
}

// Merge 把多份源的内容按参数顺序合成一份：同一主机名下按源顺序拼接候选，
// 同一条地址只留最早出现的那次。任何一份缺席（nil）不影响其他份的合并。
func Merge(maps ...map[string][]Entry) map[string][]Entry {
	out := map[string][]Entry{}
	for _, m := range maps {
		for h, rows := range m {
			for _, e := range rows {
				if !containsEntry(out[h], e) {
					out[h] = append(out[h], e)
				}
			}
		}
	}
	return out
}

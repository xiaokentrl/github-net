import type { Candidate } from '../../../bindings/github.com/xiaokentrl/github-net/internal/accelerator/models'

// 「最优可用」只有一份算法：延迟最低，并列时 IPv4 优先——和后端写 Hosts 的选址规则同一条口径。
// 详情页那格指标和队列明细的逐条都调这里；两边各写一遍就会出现「详情页一个地址、队列里另一个」。
export function pickBestCandidate(candidates: Candidate[]): Candidate | null {
  return candidates.reduce<Candidate | null>((acc, c) => {
    if (c.status !== 'available') return acc
    const better = acc === null || c.latencyMs < acc.latencyMs || (c.latencyMs === acc.latencyMs && acc.ipv6 && !c.ipv6)
    return better ? c : acc
  }, null)
}

// 「关于我们」这一屏的仓库事实，前端只有这一处写。
//
// 为什么不从后端拿：仓库地址属产品信息，不是运行状态；但它也不能只是「前端随便写的文案」，
// 所以 internal/app/about_parity_test.go 会逐字核对：
//   - REPO_HOME_URL 必须等于 `https://` + go.mod 的 module 路径（同一个仓库移动了，两边必须一起改）；
//   - 另外两个链接必须正好是首页加上固定后缀，少一段、多一段都算红；
//   - LICENSE_NAME 必须由仓库根 LICENSE 的第一行派生，改了许可证文件这边不跟着改就红。
export const REPO_HOME_URL = 'https://github.com/xiaokentrl/github-net'
export const REPO_ISSUES_URL = 'https://github.com/xiaokentrl/github-net/issues/new'
export const REPO_LICENSE_URL = 'https://github.com/xiaokentrl/github-net/blob/main/LICENSE'
export const LICENSE_NAME = 'MIT'

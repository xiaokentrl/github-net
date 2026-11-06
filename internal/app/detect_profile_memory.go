package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaokentrl/github-net/internal/config"
)

// 「记住选项」记的不是设置：弹框上次选的那一档单独放在 detect-profile.json，
// 和 config.json 同目录但互不相干，设置中心那个默认档位一个字不改。
// 用户目录不在安装包里，卸载重装不会删它，所以重启、重装之后仍然记得。
const rememberedProfileFile = "detect-profile.json"

type rememberedProfileDoc struct {
	Profile string `json:"profile"`
}

// rememberedProfilePath 跟着配置目录走：平台换了、XDG_CONFIG_HOME 变了，它也跟着变。
func rememberedProfilePath() (string, error) {
	configPath, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(configPath), rememberedProfileFile), nil
}

// ReadRememberedProfile 读上次记住的档位。没记过、文件坏了、档位认不出来都当「没记住」：
// 这个偏好只是锦上添花，不该因为一个坏文件挡住检测。
func ReadRememberedProfile() string {
	path, err := rememberedProfilePath()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var doc rememberedProfileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	profile := strings.TrimSpace(doc.Profile)
	if _, ok := config.ProfileCandidateLimit(profile); !ok {
		return ""
	}
	return profile
}

// WriteRememberedProfile 记住／忘掉：空串就是忘掉，把那份小文件删掉。
// 只认三档，别的值不写盘，免得把垃圾档位带进下一次检测。
func WriteRememberedProfile(profile string) error {
	path, err := rememberedProfilePath()
	if err != nil {
		return fmt.Errorf("拿不到偏好文件位置: %w", err)
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("忘掉检测档位失败: %w", err)
		}
		return nil
	}
	if _, ok := config.ProfileCandidateLimit(profile); !ok {
		return NewError(CodeInvalidSettings, "这个检测档位不认识，只能选省流、标准或完整检测", nil)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("建偏好目录失败: %w", err)
	}
	raw, err := json.Marshal(rememberedProfileDoc{Profile: profile})
	if err != nil {
		return fmt.Errorf("序列化检测档位失败: %w", err)
	}
	// 先写临时文件再改名：写一半被关掉也不会留下半截 JSON。
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("写检测档位失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保存检测档位失败: %w", err)
	}
	return nil
}

// GetLastDetectProfile 给弹框读上次记住的档位；没记住过给空串（界面回落到省流）。
func (s *Service) GetLastDetectProfile() string {
	return ReadRememberedProfile()
}

// SetLastDetectProfile 记住或忘掉档位：空串＝忘掉。失败明确返回，前端不把没落盘的说成记住了。
func (s *Service) SetLastDetectProfile(profile string) error {
	if err := WriteRememberedProfile(profile); err != nil {
		return ToPublicError(err)
	}
	return nil
}

package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
)

// SdkPackageMeta 是已存储 SDK 包的元信息。
// ManifestJSON 保留原始 manifest，供校验与后续转发；不落盘为独立文件，
// 而是从归档内 manifest.json 提取后与归档一并保存。
type SdkPackageMeta struct {
	Name          string                 `json:"name"`
	Version       string                 `json:"version"`
	Sha256        string                 `json:"sha256"`
	Size          int64                  `json:"size"`
	UploadedAt    time.Time              `json:"uploaded_at"`
	Routes        []SdkRouteMeta         `json:"routes,omitempty"`
	Menu          *SdkMenuMeta           `json:"menu,omitempty"`
	Dashboard     *SdkDashboardMeta      `json:"dashboard,omitempty"`
	Dependencies  map[string]string      `json:"dependencies,omitempty"`
	RunScripts    bool                   `json:"run_scripts"`
	ManifestJSON  map[string]interface{} `json:"-"`
}

// SdkRouteMeta 是 manifest.routes 的单条记录。
type SdkRouteMeta struct {
	Path   string `json:"path"`
	Module string `json:"module"`
	Title  string `json:"title,omitempty"`
}

// SdkMenuMeta 是 manifest.menu。
type SdkMenuMeta struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// SdkDashboardMeta 是 manifest.dashboard。
type SdkDashboardMeta struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// metaFileName 是每个包目录内的元信息文件。
const metaFileName = ".meta.json"

// SdkPackageStore 将 SDK 包归档持久化到磁盘：
//   StorageDir/<name>/archive（原始归档）
//   StorageDir/<name>/.meta.json（提取并校验后的元信息）
//
// 并发安全；列表按 name 排序返回。
type SdkPackageStore struct {
	mu sync.RWMutex
}

func NewSdkPackageStore() *SdkPackageStore {
	return &SdkPackageStore{}
}

// root 返回存储根目录（惰性创建）。出错时返回错误。
func (s *SdkPackageStore) root() (string, error) {
	dir := config.AppConfig.SDKPackages.StorageDir
	if dir == "" {
		dir = "./data/sdk_packages"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 SDK 包存储目录失败: %w", err)
	}
	return dir, nil
}

// nameDir 返回单个包目录；不验证名称合法性（调用方负责）。
func (s *SdkPackageStore) nameDir(root, name string) string {
	return filepath.Join(root, name)
}

func (s *SdkPackageStore) archivePath(nameDir string) string {
	return filepath.Join(nameDir, "archive")
}

// metaPath 返回包目录内的元信息文件路径。
func (s *SdkPackageStore) metaPath(nameDir string) string {
	return filepath.Join(nameDir, metaFileName)
}

// validateName 校验包名：[a-z0-9]([a-z0-9-]*[a-z0-9])?，最长 64。
// 同时阻止目录穿越（name 会直接拼入路径）。
func validateName(name string) error {
	if name == "" || len(name) > 64 {
		return fmt.Errorf("invalid package name: length must be 1..64")
	}
	if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return fmt.Errorf("invalid package name: must not contain path separators or be '.'/'..'")
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' && i > 0 && i < len(name)-1 {
			continue
		}
		return fmt.Errorf("invalid package name: must match [a-z0-9]([a-z0-9-]*[a-z0-9])?")
	}
	return nil
}

// MaxPackages 返回配置的数量上限；<=0 时使用默认 128。
func (s *SdkPackageStore) MaxPackages() int {
	if config.AppConfig.SDKPackages.MaxPackages > 0 {
		return config.AppConfig.SDKPackages.MaxPackages
	}
	return 128
}

// MaxPackageSize 返回单包大小上限（字节）；<=0 时使用默认 20 MiB。
func (s *SdkPackageStore) MaxPackageSize() int64 {
	if config.AppConfig.SDKPackages.MaxPackageSize > 0 {
		return int64(config.AppConfig.SDKPackages.MaxPackageSize)
	}
	return 20 << 20
}

// Exists 报告指定包是否已存在。
func (s *SdkPackageStore) Exists(name string) (bool, error) {
	root, err := s.root()
	if err != nil {
		return false, err
	}
	nd := s.nameDir(root, name)
	_, err = os.Stat(s.archivePath(nd))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Count 返回当前已存包数。
func (s *SdkPackageStore) Count() (int, error) {
	root, err := s.root()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n, nil
}

// Put 校验并原子写入一个包。
// data 为完整归档字节；manifest 为已提取并校验通过的原始 manifest。
// 若同名包已存在返回 ErrSdkPackageConflict。
func (s *SdkPackageStore) Put(name string, data []byte, manifest map[string]interface{}) (*SdkPackageMeta, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if int64(len(data)) > s.MaxPackageSize() {
		return nil, errPackageTooLarge
	}

	root, err := s.root()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	nd := s.nameDir(root, name)
	if _, err := os.Stat(s.archivePath(nd)); err == nil {
		return nil, ErrSdkPackageConflict
	}

	// 数量上限。
	count, err := s.Count()
	if err != nil {
		return nil, err
	}
	if count >= s.MaxPackages() {
		return nil, errTooManyPackages
	}

	sum := sha256.Sum256(data)
	meta := buildMeta(name, data, manifest, sum[:])

	// 先写临时目录，成功后再 rename 为最终目录，保证原子性。
	tmpDir := nd + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(s.archivePath(tmpDir), data, 0o644); err != nil {
		return nil, fmt.Errorf("写入归档失败: %w", err)
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("序列化元信息失败: %w", err)
	}
	if err := os.WriteFile(s.metaPath(tmpDir), metaBytes, 0o644); err != nil {
		return nil, fmt.Errorf("写入元信息失败: %w", err)
	}
	if err := os.Rename(tmpDir, nd); err != nil {
		return nil, fmt.Errorf("发布包目录失败: %w", err)
	}
	return meta, nil
}

// buildMeta 依据归档字节与 manifest 构造元信息。
func buildMeta(name string, data []byte, manifest map[string]interface{}, digest []byte) *SdkPackageMeta {
	meta := &SdkPackageMeta{
		Name:         name,
		Version:      strFromMap(manifest, "version"),
		Sha256:       hex.EncodeToString(digest),
		Size:         int64(len(data)),
		UploadedAt:   time.Now(),
		Dependencies: mapStringFromMap(manifest, "dependencies"),
		RunScripts:   boolFromMap(manifest, "run_scripts"),
		ManifestJSON: manifest,
	}

	if routes, ok := manifest["routes"].([]interface{}); ok {
		for _, r := range routes {
			if rm, ok := r.(map[string]interface{}); ok {
				meta.Routes = append(meta.Routes, SdkRouteMeta{
					Path:   strFromMap(rm, "path"),
					Module: strFromMap(rm, "module"),
					Title:  strFromMap(rm, "title"),
				})
			}
		}
	}
	if m, ok := manifest["menu"].(map[string]interface{}); ok {
		meta.Menu = &SdkMenuMeta{Label: strFromMap(m, "label"), Path: strFromMap(m, "path")}
	}
	if d, ok := manifest["dashboard"].(map[string]interface{}); ok {
		meta.Dashboard = &SdkDashboardMeta{Label: strFromMap(d, "label"), Path: strFromMap(d, "path")}
	}
	return meta
}

// GetMeta 返回包元信息（不读取归档内容）。
func (s *SdkPackageStore) GetMeta(name string) (*SdkPackageMeta, bool, error) {
	root, err := s.root()
	if err != nil {
		return nil, false, err
	}
	nd := s.nameDir(root, name)
	raw, err := os.ReadFile(s.metaPath(nd))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var meta SdkPackageMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, false, fmt.Errorf("读取包元信息失败: %w", err)
	}
	return &meta, true, nil
}

// List 返回全部包元信息，按 name 升序。
func (s *SdkPackageStore) List() ([]SdkPackageMeta, error) {
	root, err := s.root()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []SdkPackageMeta{}, nil
		}
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	metas := make([]SdkPackageMeta, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(s.metaPath(filepath.Join(root, e.Name())))
		if err != nil {
			// 元信息缺失的残缺目录：跳过（可能是中断写入残留）。
			continue
		}
		var meta SdkPackageMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		metas = append(metas, meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	return metas, nil
}

// OpenArchive 打开指定包的归档流（调用方负责 Close）。
func (s *SdkPackageStore) OpenArchive(name string) (*os.File, int64, error) {
	root, err := s.root()
	if err != nil {
		return nil, 0, err
	}
	nd := s.nameDir(root, name)
	f, err := os.Open(s.archivePath(nd))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, os.ErrNotExist
		}
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// Delete 删除指定包目录。
func (s *SdkPackageStore) Delete(name string) (bool, error) {
	root, err := s.root()
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nd := s.nameDir(root, name)
	if _, err := os.Stat(nd); os.IsNotExist(err) {
		return false, nil
	}
	if err := os.RemoveAll(nd); err != nil {
		return false, err
	}
	return true, nil
}

// ReadAllArchive 读取归档全文（供上传校验使用；大小受 MaxPackageSize 保护）。
func (s *SdkPackageStore) ReadAllArchive(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, s.MaxPackageSize()+1))
}

// 错误哨兵（供控制器映射为统一错误码）。
var (
	ErrSdkPackageConflict = fmt.Errorf("sdk package with the same name already exists")
	errPackageTooLarge    = fmt.Errorf("sdk package exceeds size limit")
	errTooManyPackages    = fmt.Errorf("sdk package count exceeds limit")
)

// 小工具：从 map 读取字段。
func strFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func boolFromMap(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func mapStringFromMap(m map[string]interface{}, key string) map[string]string {
	if v, ok := m[key].(map[string]interface{}); ok {
		out := make(map[string]string, len(v))
		for k, val := range v {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
		return out
	}
	return nil
}

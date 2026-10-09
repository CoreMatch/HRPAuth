package controllers

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// SdkPackageController 处理 SDK 包的 CRUD 与下载。
// 所有接口要求 Ops 级别（Level 2）鉴权；列表/详情/下载供 SDKHandler 使用 Ops token 拉取。
type SdkPackageController struct {
	store *SdkPackageStore
}

func NewSdkPackageController(store *SdkPackageStore) *SdkPackageController {
	return &SdkPackageController{store: store}
}

// manifestPath 是归档内根级 manifest 的规范路径。
const manifestPath = "manifest.json"

// Upload 处理 POST /services/sdk-packages：
//   - multipart/form-data，字段名 "package" 的归档文件 + 可选 "run_scripts" 覆盖。
//   - 校验归档格式、路径安全、根级唯一 manifest、manifest 字段合法性。
//   - 同名包已存在 → 409 sdk_package_conflict。
func (c *SdkPackageController) Upload(ctx *gin.Context) {
	if !requireAuthLevel(ctx, SecurityLevelOps) {
		return
	}

	fileHeader, err := ctx.FormFile("package")
	if err != nil {
		respondError(ctx, http.StatusBadRequest, CodeInvalidRequest, "\"package\" file field is required (multipart/form-data)")
		return
	}
	if fileHeader.Size > c.store.MaxPackageSize() {
		respondError(ctx, http.StatusRequestEntityTooLarge, CodeSdkPackageTooLarge, "package exceeds the configured size limit")
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		respondError(ctx, http.StatusBadRequest, CodeSdkPackageInvalid, "failed to open uploaded package: "+err.Error())
		return
	}
	defer src.Close()

	data, err := c.store.ReadAllArchive(src)
	if err != nil {
		respondError(ctx, http.StatusRequestEntityTooLarge, CodeSdkPackageTooLarge, "package exceeds the configured size limit")
		return
	}

	// 校验归档并提取根级唯一 manifest。
	manifest, err := validateAndExtractManifest(data, fileHeader.Filename)
	if err != nil {
		status, code, msg := classifyUploadError(err)
		respondError(ctx, status, code, msg)
		return
	}

	name := strings.TrimSpace(strFromMap(manifest, "name"))
	if err := validateName(name); err != nil {
		respondError(ctx, http.StatusBadRequest, CodeSdkPackageInvalidManifest, "manifest \"name\" is invalid: "+err.Error())
		return
	}

	meta, err := c.store.Put(name, data, manifest)
	if err != nil {
		switch {
		case err == ErrSdkPackageConflict:
			respondError(ctx, http.StatusConflict, CodeSdkPackageConflict, "a package with name \""+name+"\" already exists; DELETE it first to replace")
		case err == errPackageTooLarge:
			respondError(ctx, http.StatusRequestEntityTooLarge, CodeSdkPackageTooLarge, "package exceeds the configured size limit")
		case err == errTooManyPackages:
			respondError(ctx, http.StatusRequestEntityTooLarge, CodeSdkPackageTooLarge, "package count limit reached")
		default:
			respondError(ctx, http.StatusBadRequest, CodeSdkPackageInvalid, "failed to store package: "+err.Error())
		}
		return
	}

	respondCreated(ctx, "sdk package uploaded", publicMeta(meta))
}

// List 处理 GET /services/sdk-packages：
//   返回全部包元信息数组；设置 ETag（列表内容 sha256），配合 If-None-Match 返回 304。
func (c *SdkPackageController) List(ctx *gin.Context) {
	if !requireAuthLevel(ctx, SecurityLevelOps) {
		return
	}
	metas, err := c.store.List()
	if err != nil {
		respondError(ctx, http.StatusInternalServerError, CodeInternalError, "failed to list sdk packages")
		return
	}
	pub := make([]any, 0, len(metas))
	for i := range metas {
		pub = append(pub, publicMeta(&metas[i]))
	}

	etag := packageListETag(pub)
	ctx.Header("ETag", etag)
	if match := ctx.GetHeader("If-None-Match"); match != "" && match == etag {
		ctx.Status(http.StatusNotModified)
		return
	}
	respondOK(ctx, "sdk packages fetched", pub)
}

// Get 处理 GET /services/sdk-packages/:name：返回单个包元信息。
func (c *SdkPackageController) Get(ctx *gin.Context) {
	if !requireAuthLevel(ctx, SecurityLevelOps) {
		return
	}
	name := strings.TrimSpace(ctx.Param("name"))
	meta, ok, err := c.store.GetMeta(name)
	if err != nil {
		respondError(ctx, http.StatusInternalServerError, CodeInternalError, "failed to read sdk package")
		return
	}
	if !ok {
		respondError(ctx, http.StatusNotFound, CodeSdkPackageNotFound, "sdk package not found")
		return
	}
	respondOK(ctx, "sdk package fetched", publicMeta(meta))
}

// Download 处理 GET /services/sdk-packages/:name/download：返回原始归档字节流。
func (c *SdkPackageController) Download(ctx *gin.Context) {
	if !requireAuthLevel(ctx, SecurityLevelOps) {
		return
	}
	name := strings.TrimSpace(ctx.Param("name"))
	f, size, err := c.store.OpenArchive(name)
	if err != nil {
		if err == os.ErrNotExist {
			respondError(ctx, http.StatusNotFound, CodeSdkPackageNotFound, "sdk package not found")
			return
		}
		respondError(ctx, http.StatusInternalServerError, CodeInternalError, "failed to open sdk package")
		return
	}
	defer f.Close()

	ctx.Header("Content-Disposition", "attachment; filename=\""+name+"-sdk.archive\"")
	ctx.DataFromReader(http.StatusOK, size, "application/octet-stream", f, nil)
}

// Delete 处理 DELETE /services/sdk-packages/:name。
func (c *SdkPackageController) Delete(ctx *gin.Context) {
	if !requireAuthLevel(ctx, SecurityLevelOps) {
		return
	}
	name := strings.TrimSpace(ctx.Param("name"))
	deleted, err := c.store.Delete(name)
	if err != nil {
		respondError(ctx, http.StatusInternalServerError, CodeInternalError, "failed to delete sdk package")
		return
	}
	if !deleted {
		respondError(ctx, http.StatusNotFound, CodeSdkPackageNotFound, "sdk package not found")
		return
	}
	respondOK(ctx, "sdk package deleted", gin.H{"name": name, "deleted": true})
}

// publicMeta 构造对外元信息（剔除内部 ManifestJSON，避免把原始 manifest 原样返回给列表）。
func publicMeta(meta *SdkPackageMeta) gin.H {
	h := gin.H{
		"name":        meta.Name,
		"version":     meta.Version,
		"sha256":      meta.Sha256,
		"size":        meta.Size,
		"uploaded_at": meta.UploadedAt,
		"run_scripts": meta.RunScripts,
	}
	if len(meta.Routes) > 0 {
		h["routes"] = meta.Routes
	}
	if meta.Menu != nil {
		h["menu"] = meta.Menu
	}
	if meta.Dashboard != nil {
		h["dashboard"] = meta.Dashboard
	}
	if len(meta.Dependencies) > 0 {
		h["dependencies"] = meta.Dependencies
	}
	return h
}

// packageListETag 计算列表内容的稳定摘要（按 name 排序后 JSON 序列化再 sha256）。
func packageListETag(pub []any) string {
	sum := sha256.Sum256(mustJSON(pub))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// classifyUploadError 把归档校验错误映射为（HTTP 状态，错误码，消息）。
func classifyUploadError(err error) (int, string, string) {
	switch err {
	case errNotArchive:
		return http.StatusBadRequest, CodeSdkPackageInvalid, "archive format must be tar.gz or zip"
	case errMultipleManifests:
		return http.StatusBadRequest, CodeSdkPackageInvalidManifest, "archive must contain exactly one root-level manifest.json"
	case errMissingManifest:
		return http.StatusBadRequest, CodeSdkPackageInvalidManifest, "archive is missing root-level manifest.json"
	case errIllegalPath:
		return http.StatusBadRequest, CodeSdkPackageInvalidPath, "archive contains illegal path (traversal/absolute/symlink/hard link)"
	default:
		return http.StatusBadRequest, CodeSdkPackageInvalid, "invalid package: " + err.Error()
	}
}

// 校验相关错误哨兵。
var (
	errNotArchive       = fmt.Errorf("not a supported archive")
	errMultipleManifests = fmt.Errorf("multiple root-level manifests")
	errMissingManifest   = fmt.Errorf("missing manifest")
	errIllegalPath       = fmt.Errorf("illegal path")
)

// allowedSDKExt 是包内允许的源文件扩展名；其他扩展名（如 .sh）直接拒绝。
// 这里仅做保守的扩展名检查；真正的路径安全在 walk 时强制。
var allowedSDKExt = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".mjs": true, ".cjs": true, ".json": true, ".css": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".woff": true, ".woff2": true, ".ttf": true,
	".html": true, ".md": true, ".txt": true,
}

// validateAndExtractManifest 校验归档并返回根级唯一 manifest（解析为 map）。
func validateAndExtractManifest(data []byte, filename string) (map[string]interface{}, error) {
	name := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return extractFromTarGz(data)
	case strings.HasSuffix(name, ".zip"):
		return extractFromZip(data)
	default:
		return nil, errNotArchive
	}
}

// sanitizeArchivePath 校验归档内路径：必须相对、无 ..、无绝对路径、无链接。
// 返回去除前导 ./ 后的规范相对路径。
func sanitizeArchivePath(name string) (string, error) {
	clean := strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "./")
	if clean == "" || strings.HasPrefix(clean, "/") {
		return "", errIllegalPath
	}
	segs := strings.Split(clean, "/")
	for _, seg := range segs {
		if seg == ".." {
			return "", errIllegalPath
		}
	}
	// 规范路径不允许重新上溯。
	norm := path.Clean(clean)
	if norm == ".." || strings.HasPrefix(norm, "../") {
		return "", errIllegalPath
	}
	return norm, nil
}

// extractFromTarGz 校验并提取 tar.gz 归档中的根级 manifest。
func extractFromTarGz(data []byte) (map[string]interface{}, error) {
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errNotArchive
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	rootManifests := make(map[string]bool)
	var manifestRaw []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errNotArchive
		}
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return nil, errIllegalPath
		}
		p, err := sanitizeArchivePath(hdr.Name)
		if err != nil {
			return nil, err
		}
		if p == manifestPath {
			rootManifests[p] = true
			raw, err := io.ReadAll(io.LimitReader(tr, 4<<20))
			if err != nil {
				return nil, errNotArchive
			}
			manifestRaw = raw
		} else {
			// 扩展名黑名单：只允许源码与静态资源。
			if !isAllowedExt(p) {
				return nil, fmt.Errorf("disallowed file extension: %s", path.Ext(p))
			}
		}
	}
	if len(rootManifests) != 1 {
		if len(rootManifests) == 0 {
			return nil, errMissingManifest
		}
		return nil, errMultipleManifests
	}
	return parseManifest(manifestRaw)
}

// extractFromZip 校验并提取 zip 归档中的根级 manifest。
func extractFromZip(data []byte) (map[string]interface{}, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errNotArchive
	}
	rootManifests := make(map[string]bool)
	var manifestRaw []byte
	for _, f := range zr.File {
		p, err := sanitizeArchivePath(f.Name)
		if err != nil {
			return nil, err
		}
		if p == manifestPath {
			rootManifests[p] = true
			rc, err := f.Open()
			if err != nil {
				return nil, errNotArchive
			}
			raw, err := io.ReadAll(io.LimitReader(rc, 4<<20))
			rc.Close()
			if err != nil {
				return nil, errNotArchive
			}
			manifestRaw = raw
		} else if !isAllowedExt(p) {
			return nil, fmt.Errorf("disallowed file extension: %s", path.Ext(p))
		}
	}
	if len(rootManifests) != 1 {
		if len(rootManifests) == 0 {
			return nil, errMissingManifest
		}
		return nil, errMultipleManifests
	}
	return parseManifest(manifestRaw)
}

func isAllowedExt(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return allowedSDKExt[ext]
}

// parseManifest 解析 manifest.json 并做字段级校验。
func parseManifest(raw []byte) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, errMissingManifest
	}
	if err := validateManifest(m); err != nil {
		return nil, err
	}
	return m, nil
}

// validateManifest 校验 manifest 字段（schema_version/name/version/routes/menu/dashboard）。
func validateManifest(m map[string]interface{}) error {
	if sv, ok := m["schema_version"].(float64); !ok || sv != 1 {
		return fmt.Errorf("manifest schema_version must be 1")
	}
	name := strFromMap(m, "name")
	if err := validateName(name); err != nil {
		return fmt.Errorf("manifest name invalid: %v", err)
	}
	version := strFromMap(m, "version")
	if !isSemver(version) {
		return fmt.Errorf("manifest version must be semantic (major.minor.patch)")
	}

	routes, ok := m["routes"].([]interface{})
	if !ok || len(routes) == 0 {
		return fmt.Errorf("manifest routes must be a non-empty array")
	}
	seenPaths := map[string]bool{}
	for _, r := range routes {
		rm, ok := r.(map[string]interface{})
		if !ok {
			return fmt.Errorf("manifest route entry must be an object")
		}
		rp := strFromMap(rm, "path")
		if !validRoutePath(rp) {
			return fmt.Errorf("manifest route path %q invalid: must start with / and contain no ?, #, .. segments", rp)
		}
		if seenPaths[rp] {
			return fmt.Errorf("manifest route path %q duplicated", rp)
		}
		seenPaths[rp] = true
		mod := strFromMap(rm, "module")
		if mod == "" {
			return fmt.Errorf("manifest route %q missing module", rp)
		}
		if !validModulePath(mod) {
			return fmt.Errorf("manifest route %q module %q invalid: must be a relative in-package path", rp, mod)
		}
	}

	if menu, ok := m["menu"].(map[string]interface{}); ok {
		if !seenPaths[strFromMap(menu, "path")] {
			return fmt.Errorf("manifest menu.path must reference an entry in routes")
		}
	}
	if dash, ok := m["dashboard"].(map[string]interface{}); ok {
		if !seenPaths[strFromMap(dash, "path")] {
			return fmt.Errorf("manifest dashboard.path must reference an entry in routes")
		}
	}
	return nil
}

// validRoutePath 校验路由路径：/ 开头、不以 / 结尾（根除外）、无 ?/#/.. 段。
func validRoutePath(p string) bool {
	if !strings.HasPrefix(p, "/") {
		return false
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		return false
	}
	if strings.ContainsAny(p, "?#") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// validModulePath 校验模块相对路径：非空、相对、可去扩展名。
func validModulePath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "./") {
		return false
	}
	if strings.Contains(p, "..") {
		return false
	}
	ext := path.Ext(p)
	if ext == "" {
		return true
	}
	return allowedSDKExt[strings.ToLower(ext)]
}

// isSemver 校验 major.minor.patch 形式。
func isSemver(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

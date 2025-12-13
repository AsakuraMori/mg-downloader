package pocketShonenmagazine

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DownloadConfig 下载配置
type DownloadConfig struct {
	URL          string
	OutputDir    string
	ScrambleSeed int
	TileCount    int
	Timeout      time.Duration
	Client       *http.Client
}

type Cookie struct {
	Domain         string  `json:"domain"`
	ExpirationDate float64 `json:"expirationDate"`
	HostOnly       bool    `json:"hostOnly"`
	HTTPOnly       bool    `json:"httpOnly"`
	Name           string  `json:"name"`
	Path           string  `json:"path"`
	SameSite       string  `json:"sameSite"`
	Secure         bool    `json:"secure"`
	Session        bool    `json:"session"`
	StoreID        string  `json:"storeId"`
	Value          string  `json:"value"`
}

// EpisodeData Shonen Magazine API 响应结构
type ShonenMagazineEpisodeData struct {
	ScrambleSeed int      `json:"scramble_seed"`
	PageList     []string `json:"page_list"`
}

var EpisodeData *ShonenMagazineEpisodeData
var Cookies *[]Cookie

// Xorshift32 随机数生成器
type Xorshift32 struct {
	state uint32
}

// NewXorshift32 创建新的Xorshift32实例
func NewXorshift32(seed uint32) *Xorshift32 {
	return &Xorshift32{state: seed}
}

// Next 生成下一个随机数
func (x *Xorshift32) Next() uint32 {
	x.state ^= x.state << 13
	x.state ^= x.state >> 17
	x.state ^= x.state << 5
	return x.state
}

// ShuffleOrder 生成乱序数组
func ShuffleOrder(count int, seed int) []int {
	gen := NewXorshift32(uint32(seed))
	pairs := make([][2]int, count)

	for i := 0; i < count; i++ {
		pairs[i] = [2]int{int(gen.Next()), i}
	}

	// 排序
	for i := 0; i < count; i++ {
		for j := i + 1; j < count; j++ {
			if pairs[i][0] > pairs[j][0] {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	// 提取索引
	result := make([]int, count)
	for i := 0; i < count; i++ {
		result[i] = pairs[i][1]
	}

	return result
}

// CalculateDescrambleDimensions 计算解扰后的图片尺寸
func CalculateDescrambleDimensions(originalWidth, originalHeight, tileCount int) (int, int, bool) {
	const y = 8

	if originalWidth < tileCount*y || originalHeight < tileCount*y {
		return 0, 0, false
	}

	tempWidth := originalWidth / y
	tempHeight := originalHeight / y

	finalTileableWidth := tempWidth / tileCount
	finalTileableHeight := tempHeight / tileCount

	return finalTileableWidth * y, finalTileableHeight * y, true
}

// UnscrambleImage 解扰图片
func UnscrambleImage(img image.Image, scrambleSeed, tileCount int) (image.Image, error) {
	bounds := img.Bounds()
	originalWidth := bounds.Dx()
	originalHeight := bounds.Dy()

	// 计算解扰后的尺寸
	tileW, tileH, ok := CalculateDescrambleDimensions(originalWidth, originalHeight, tileCount)
	if !ok {
		return img, nil // 图片太小，直接返回原图
	}

	leftoverW := originalWidth - tileW*tileCount
	leftoverH := originalHeight - tileH*tileCount

	// 创建新图片
	unscrambled := image.NewRGBA(bounds)

	// 获取乱序
	order := ShuffleOrder(tileCount*tileCount, scrambleSeed)

	// 重新排列图块
	for i := 0; i < len(order); i++ {
		sourceTileIndex := order[i]
		destTileIndex := i

		// 计算源图块位置
		srcX := (sourceTileIndex % tileCount) * tileW
		srcY := (sourceTileIndex / tileCount) * tileH

		// 计算目标图块位置
		destX := (destTileIndex % tileCount) * tileW
		destY := (destTileIndex / tileCount) * tileH

		// 复制图块
		for y := 0; y < tileH; y++ {
			for x := 0; x < tileW; x++ {
				color := img.At(srcX+x, srcY+y)
				unscrambled.Set(destX+x, destY+y, color)
			}
		}
	}

	// 复制右侧剩余垂直条
	if leftoverW > 0 {
		srcX := originalWidth - leftoverW
		for y := 0; y < originalHeight; y++ {
			for x := 0; x < leftoverW; x++ {
				color := img.At(srcX+x, y)
				unscrambled.Set(srcX+x, y, color)
			}
		}
	}

	// 复制底部剩余水平条
	if leftoverH > 0 {
		srcY := originalHeight - leftoverH
		for y := 0; y < leftoverH; y++ {
			for x := 0; x < originalWidth; x++ {
				color := img.At(x, srcY+y)
				unscrambled.Set(x, srcY+y, color)
			}
		}
	}

	return unscrambled, nil
}

// DownloadImage 下载单个图片
func DownloadImage(url string, client *http.Client, timeout time.Duration) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/webp,image/apng,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://pocket.shonenmagazine.com/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载图片失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("服务器返回非200状态码: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// ProcessImage 处理图片（解扰）
func ProcessImage(imgData []byte, scrambleSeed int, tileCount int) ([]byte, error) {
	// 解码图片
	img, err := jpeg.Decode(strings.NewReader(string(imgData)))
	if err != nil {
		return nil, fmt.Errorf("解码图片失败: %w", err)
	}

	// 解扰图片
	processedImg, err := UnscrambleImage(img, scrambleSeed, tileCount)
	if err != nil {
		return nil, fmt.Errorf("解扰图片失败: %w", err)
	}

	// 编码为JPEG
	var buf strings.Builder
	err = jpeg.Encode(&buf, processedImg, &jpeg.Options{Quality: 95})
	if err != nil {
		return nil, fmt.Errorf("编码图片失败: %w", err)
	}

	return []byte(buf.String()), nil
}

// SaveImage 保存图片到文件
func SaveImage(data []byte, filepath string) error {
	// 创建目录
	dir := filepath[:len(filepath)-len(filepath[strings.LastIndex(filepath, "/")+1:])]
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
	}

	// 写入文件
	return os.WriteFile(filepath, data, 0644)
}

// ComputeHash 计算参数哈希
func ComputeHash(params map[string]string, seed string) (string, error) {
	// 获取并排序所有键
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 计算每个键值对的哈希
	var paramHashes []string
	for _, key := range keys {
		value := params[key]

		// key的SHA-256
		keyHash := sha256.Sum256([]byte(key))
		keyHashStr := hex.EncodeToString(keyHash[:])

		// value的SHA-512
		valueHash := sha512.Sum512([]byte(value))
		valueHashStr := hex.EncodeToString(valueHash[:])

		paramHashes = append(paramHashes, keyHashStr+"_"+valueHashStr)
	}

	// 合并并计算SHA-256
	combined := strings.Join(paramHashes, ",")
	combinedHash := sha256.Sum256([]byte(combined))

	// 添加种子并计算最终的SHA-512
	finalInput := hex.EncodeToString(combinedHash[:]) + seed
	finalHash := sha512.Sum512([]byte(finalInput))

	return hex.EncodeToString(finalHash[:]), nil
}

func Load() ([]Cookie, error) {
	file, err := os.Open("./cookies/cookie.ps.json")
	if err != nil {
		return nil, fmt.Errorf("could not open cookie file: %v", err)
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("could not read cookie file: %v", err)
	}

	var cookies []Cookie
	if err := json.Unmarshal(bytes, &cookies); err != nil {
		return nil, fmt.Errorf("could not parse cookie file: %v", err)
	}

	return cookies, nil
}

// 辅助函数
func extractEpisodeID(url string) string {
	regex := regexp.MustCompile(`episode/(\d+)`)
	matches := regex.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var UrlString string

func GetFirstPageFromPocketShonenmagazine(urlstr string) (string, string, error) {
	// 提取episode ID
	episodeID := extractEpisodeID(urlstr)
	if episodeID == "" {
		return "", "", fmt.Errorf("无效的URL: 无法提取episode ID")
	}

	client := &http.Client{
		Timeout: 30 * time.Second, // 设置超时时间
	}

	// 首先访问网页获取标题
	pageReq, err := http.NewRequest("GET", urlstr, nil)
	if err != nil {
		return "", "", fmt.Errorf("创建页面请求失败: %w", err)
	}

	pageReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	pageReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	pageReq.Header.Set("Accept-Language", "en-US,en;q=0.9")

	pageResp, err := client.Do(pageReq)
	if err != nil {
		return "", "", fmt.Errorf("请求页面失败: %w", err)
	}
	defer pageResp.Body.Close()

	if pageResp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("页面请求失败，状态码: %d", pageResp.StatusCode)
	}

	// 读取HTML内容
	htmlData, err := io.ReadAll(pageResp.Body)
	if err != nil {
		return "", "", fmt.Errorf("读取页面内容失败: %w", err)
	}

	// 解析HTML获取标题
	title := extractTitleFromHTML(string(htmlData))
	if title == "" {
		return "", "", fmt.Errorf("无法从HTML提取标题")
	}

	// 构建API URL
	apiURL := fmt.Sprintf("https://api.pocket.shonenmagazine.com/web/episode/viewer?episode_id=%s", episodeID)

	// 计算API签名
	params := map[string]string{
		"episode_id": episodeID,
	}
	seed := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855_cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e"
	hash1, err := ComputeHash(params, seed)
	if err != nil {
		return "", "", fmt.Errorf("计算API签名失败: %w", err)
	}

	// 创建API请求
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("创建API请求失败: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("x-manga-is-crawler", "false")
	req.Header.Set("x-manga-platform", "3")
	req.Header.Set("x-manga-hash", hash1)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://pocket.shonenmagazine.com/")
	cookies, err := Load()
	if err != nil {
		return "", "", err
	}
	//Cookies = &cookies
	for _, cookie := range cookies {
		req.AddCookie(&http.Cookie{
			Name:  cookie.Name,
			Value: cookie.Value,
		})
	}
	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("请求API失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("API请求失败，状态码: %d", resp.StatusCode)
	}

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("读取API响应失败: %w", err)
	}

	// 解析JSON响应到结构体
	var episodeData ShonenMagazineEpisodeData
	if err := json.Unmarshal(body, &episodeData); err != nil {
		return "", "", fmt.Errorf("解析API响应JSON失败: %w", err)
	}

	// 保存到全局变量
	EpisodeData = &episodeData

	// 获取第一页图片URL
	var firstPageURL string
	if len(episodeData.PageList) > 0 {
		firstPageURL = episodeData.PageList[0]
	}

	if firstPageURL == "" {
		return "", "", fmt.Errorf("无法获取第一页图片URL")
	}

	// 下载第一页图片
	imgReq, err := http.NewRequest("GET", firstPageURL, nil)
	if err != nil {
		return title, "", fmt.Errorf("创建图片请求失败: %w", err)
	}

	imgReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	imgReq.Header.Set("Accept", "image/webp,image/apng,image/*,*/*;q=0.8")
	imgReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
	imgReq.Header.Set("Referer", "https://pocket.shonenmagazine.com/")

	imgResp, err := client.Do(imgReq)
	if err != nil {
		return title, "", fmt.Errorf("下载图片失败: %w", err)
	}
	defer imgResp.Body.Close()

	if imgResp.StatusCode != http.StatusOK {
		return title, "", fmt.Errorf("图片下载失败，状态码: %d", imgResp.StatusCode)
	}

	// 读取图片数据
	imgData, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return title, "", fmt.Errorf("读取图片数据失败: %w", err)
	}

	// 处理图片（如果需要解扰）
	var processedImgData []byte
	if episodeData.ScrambleSeed > 0 {
		processedImgData, err = ProcessImage(imgData, episodeData.ScrambleSeed, 4) // tileCount默认为4
		if err != nil {
			return title, "", fmt.Errorf("处理图片失败: %w", err)
		}
	} else {
		processedImgData = imgData
	}

	// 转换为base64
	base64Str := base64.StdEncoding.EncodeToString(processedImgData)

	// 添加data:image/jpeg;base64,前缀
	fullBase64 := "data:image/jpeg;base64," + base64Str

	return title, fullBase64, nil
}

// extractTitleFromHTML 从HTML中提取标题
func extractTitleFromHTML(html string) string {
	// 正则表达式匹配<title>标签
	re := regexp.MustCompile(`<title>(.*?)</title>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		title := matches[1]
		// 清理标题，移除多余的空格和换行
		title = strings.TrimSpace(title)
		title = strings.ReplaceAll(title, "\n", "")
		title = strings.ReplaceAll(title, "\r", "")

		//// 去除常见的网站名称后缀
		//removeSuffixes := []string{
		//	"| 少年マガジン",
		//	"| マンガポケット",
		//	"| マンガ図書館",
		//	"| マガジン",
		//	"| Pocket",
		//	"| Shonen Magazine",
		//	"- 少年マガジン",
		//	"- マンガポケット",
		//	"- マンガ図書館",
		//	"- マガジン",
		//	"- Pocket",
		//	"- Shonen Magazine",
		//}
		//
		//for _, suffix := range removeSuffixes {
		//	if idx := strings.Index(title, suffix); idx != -1 {
		//		title = strings.TrimSpace(title[:idx])
		//		break
		//	}
		//}

		return title
	}

	return ""
}

//func DownloadMangaFromShonenmagazine(outDir string) error {
//	err := DownloadMangaImages(UrlString, outDir)
//	if err != nil {
//		return err
//	}
//	return nil
//}

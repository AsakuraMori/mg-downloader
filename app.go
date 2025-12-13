package main

import (
	"context"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	cd "mg-Downloader/pkg/comicDays"
	of "mg-Downloader/pkg/ourfeel"
	ps "mg-Downloader/pkg/pocketShonenmagazine"
)

type ComicInfo struct {
	Mode      string `json:"mode"`
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
	PageURL   string `json:"page_url"`
}

type DownloadProgress struct {
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Title   string `json:"title"`
	Status  string `json:"status"`
}

type App struct {
	ctx                context.Context
	currentMode        string
	isDownloading      bool
	forceStop          bool // 新增：强制停止标志
	downloadCancelChan chan struct{}
	progressStopChan   chan struct{}
	downloadMutex      sync.RWMutex
	eventListeners     map[string]func()
	downloadSessionId  int64     // 新增：下载会话ID
	lastCancelTime     time.Time // 新增：最后取消时间
}

func NewApp() *App {
	return &App{
		downloadCancelChan: make(chan struct{}, 1),
		progressStopChan:   make(chan struct{}, 1),
		eventListeners:     make(map[string]func()),
		forceStop:          false,
		downloadSessionId:  0,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	log.Println("[Backend] 应用启动完成")
}

func (a *App) SearchComics(mode string, query string) ([]ComicInfo, error) {
	// 检查是否刚刚被取消
	a.downloadMutex.RLock()
	recentlyCancelled := !a.lastCancelTime.IsZero() && time.Since(a.lastCancelTime) < 2*time.Second
	a.downloadMutex.RUnlock()

	if recentlyCancelled {
		log.Println("[Backend] ⚠️ 最近有取消操作，等待清理")
		time.Sleep(200 * time.Millisecond)
	}

	time.Sleep(1 * time.Second)

	var comics []ComicInfo
	log.Printf("[Backend] 搜索: %s - %s", mode, query)

	switch mode {
	case "comicDays":
		mgTitle, picSrc, err := cd.GetFirstPageFromComicDays(query)
		if err != nil {
			return nil, err
		}
		comics = []ComicInfo{
			{Mode: mode, Title: mgTitle, Thumbnail: picSrc, PageURL: query},
		}
	case "ourfeel":
		mgTitle, picSrc, err := of.GetFirstPageFromOurfeel(query)
		if err != nil {
			return nil, err
		}
		comics = []ComicInfo{
			{Mode: mode, Title: mgTitle, Thumbnail: picSrc, PageURL: query},
		}
	case "PocketShonenmagazine":
		mgTitle, picSrc, err := ps.GetFirstPageFromPocketShonenmagazine(query)
		if err != nil {
			return nil, err
		}
		comics = []ComicInfo{
			{Mode: mode, Title: mgTitle, Thumbnail: picSrc, PageURL: query},
		}
	default:
		return nil, fmt.Errorf("未知模式: %s", mode)
	}

	var filtered []ComicInfo
	for _, comic := range comics {
		if query == "" || contains(comic.Title, query) {
			filtered = append(filtered, comic)
		}
	}

	return filtered, nil
}

func (a *App) DownloadComicPage(comic ComicInfo) error {
	log.Printf("[Backend] 🚀 开始下载: %s", comic.Title)

	// 检查强制停止标志
	if a.isForceStop() {
		log.Println("[Backend] ❌ 强制停止中，拒绝新下载")
		return fmt.Errorf("下载已被强制停止")
	}

	// 生成新的下载会话ID
	a.downloadMutex.Lock()
	sessionId := a.downloadSessionId + 1
	a.downloadSessionId = sessionId
	a.isDownloading = true
	a.forceStop = false
	a.currentMode = comic.Mode
	a.downloadMutex.Unlock()

	log.Printf("[Backend] 📋 下载会话 ID: %d", sessionId)

	// 重置所有通道
	a.clearAllChannels()

	// 选择保存路径
	outDir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "保存路径",
	})
	if err != nil {
		a.cleanupDownloadState()
		return fmt.Errorf("选择路径失败: %w", err)
	}

	if outDir == "" {
		a.cleanupDownloadState()
		return fmt.Errorf("未选择路径")
	}

	// 获取总页数
	var totalPages int
	var comicTitle string

	switch comic.Mode {
	case "comicDays":
		if cd.COMIC_DAYS_INFO == nil {
			a.cleanupDownloadState()
			return fmt.Errorf("请先搜索")
		}
		totalPages = len(cd.COMIC_DAYS_INFO.Pages)
		comicTitle = comic.Title
	case "ourfeel":
		if of.OURFEEL_INFO == nil {
			a.cleanupDownloadState()
			return fmt.Errorf("请先搜索")
		}
		totalPages = len(of.OURFEEL_INFO.Pages)
		comicTitle = comic.Title
	case "PocketShonenmagazine":
		if ps.EpisodeData == nil {
			a.cleanupDownloadState()
			return fmt.Errorf("请先搜索")
		}
		totalPages = len(ps.EpisodeData.PageList)
		fmt.Println(totalPages)
		comicTitle = comic.Title
	default:
		a.cleanupDownloadState()

		return fmt.Errorf("不支持的模式: %s", comic.Mode)
	}

	// 发送开始进度
	if err := a.sendProgressSafely(DownloadProgress{
		Current: 0,
		Total:   totalPages,
		Title:   comicTitle,
		Status:  "started",
	}, sessionId); err != nil {
		a.cleanupDownloadState()
		return err
	}

	// 执行下载
	var downloadErr error
	if comic.Mode == "comicDays" {
		downloadErr = a.downloadComicDays(outDir, totalPages, comicTitle, sessionId)
	} else if comic.Mode == "ourfeel" {
		downloadErr = a.downloadOurfeel(outDir, totalPages, comicTitle, sessionId)
	} else if comic.Mode == "PocketShonenmagazine" {
		downloadErr = a.downloadPocketShonenmagazine(outDir, totalPages, comicTitle, sessionId)
	}

	// 清理状态
	a.cleanupDownloadState()

	return downloadErr
}

func (a *App) downloadComicDays(outDir string, totalPages int, title string, sessionId int64) error {
	log.Printf("[Backend] 下载ComicDays: %s (%d页) [会话:%d]", title, totalPages, sessionId)

	for i, page := range cd.COMIC_DAYS_INFO.Pages {
		// 检查是否应该停止（带会话ID检查）
		if a.shouldStopDownload(sessionId) {
			log.Printf("[Backend] ❌ 会话 %d 检测到停止，退出下载", sessionId)
			return nil
		}

		// 检查取消通道
		select {
		case <-a.downloadCancelChan:
			log.Printf("[Backend] ❌ 会话 %d 收到取消信号", sessionId)
			a.setForceStop()
			return nil
		case <-a.progressStopChan:
			log.Printf("[Backend] ❌ 会话 %d 收到停止进度信号", sessionId)
			a.setForceStop()
		default:
		}

		pageNum := i + 1

		// 发送进度
		if err := a.sendProgressSafely(DownloadProgress{
			Current: pageNum,
			Total:   totalPages,
			Title:   title,
			Status:  "downloading",
		}, sessionId); err != nil {
			log.Printf("[Backend] ⚠️ 发送进度失败: %v", err)
		}

		// 处理页面
		page.Process(cd.COMIC_DAYS_INFO.NetworkClient, cd.COMIC_DAYS_INFO.Cookies, outDir, pageNum)

		// 每个页面后再次检查
		if a.shouldStopDownload(sessionId) {
			log.Printf("[Backend] ❌ 页面处理后检测到停止")
			return nil
		}
	}

	// 发送完成
	if !a.isForceStop() {
		a.sendProgressSafely(DownloadProgress{
			Current: totalPages,
			Total:   totalPages,
			Title:   title,
			Status:  "completed",
		}, sessionId)
	}

	return nil
}

func (a *App) downloadOurfeel(outDir string, totalPages int, title string, sessionId int64) error {
	log.Printf("[Backend] 下载Ourfeel: %s (%d页) [会话:%d]", title, totalPages, sessionId)

	for i, page := range of.OURFEEL_INFO.Pages {
		// 检查是否应该停止
		if a.shouldStopDownload(sessionId) {
			log.Printf("[Backend] ❌ 会话 %d 检测到停止，退出下载", sessionId)
			return nil
		}

		// 检查取消通道
		select {
		case <-a.downloadCancelChan:
			log.Printf("[Backend] ❌ 会话 %d 收到取消信号", sessionId)
			a.setForceStop()
			return nil
		case <-a.progressStopChan:
			log.Printf("[Backend] ❌ 会话 %d 收到停止进度信号", sessionId)
			a.setForceStop()
		default:
		}

		pageNum := i + 1

		// 发送进度
		if err := a.sendProgressSafely(DownloadProgress{
			Current: pageNum,
			Total:   totalPages,
			Title:   title,
			Status:  "downloading",
		}, sessionId); err != nil {
			log.Printf("[Backend] ⚠️ 发送进度失败: %v", err)
		}

		// 处理页面
		page.Process(of.OURFEEL_INFO.NetworkClient, outDir, pageNum)

		// 每个页面后再次检查
		if a.shouldStopDownload(sessionId) {
			log.Printf("[Backend] ❌ 页面处理后检测到停止")
			return nil
		}
	}

	// 发送完成
	if !a.isForceStop() {
		a.sendProgressSafely(DownloadProgress{
			Current: totalPages,
			Total:   totalPages,
			Title:   title,
			Status:  "completed",
		}, sessionId)
	}

	return nil
}

func (a *App) downloadPocketShonenmagazine(outDir string, totalPages int, title string, sessionId int64) error {
	log.Printf("[Backend] 下载PocketShonenmagazine: %s (%d页) [会话:%d]", title, totalPages, sessionId)
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	config := ps.DownloadConfig{
		OutputDir: outDir,
		TileCount: 4,
		Timeout:   30 * time.Second,
		Client:    client,
	}
	for i, imgURL := range ps.EpisodeData.PageList {
		// 检查是否应该停止
		if a.shouldStopDownload(sessionId) {
			log.Printf("[Backend] ❌ 会话 %d 检测到停止，退出下载", sessionId)
			return nil
		}

		// 检查取消通道
		select {
		case <-a.downloadCancelChan:
			log.Printf("[Backend] ❌ 会话 %d 收到取消信号", sessionId)
			a.setForceStop()
			return nil
		case <-a.progressStopChan:
			log.Printf("[Backend] ❌ 会话 %d 收到停止进度信号", sessionId)
			a.setForceStop()
		default:
		}

		pageNum := i + 1

		// 发送进度
		if err := a.sendProgressSafely(DownloadProgress{
			Current: pageNum,
			Total:   totalPages,
			Title:   title,
			Status:  "downloading",
		}, sessionId); err != nil {
			log.Printf("[Backend] ⚠️ 发送进度失败: %v", err)
		}

		// 处理页面
		//page.Process(of.OURFEEL_INFO.NetworkClient, outDir, pageNum)
		if len(ps.EpisodeData.PageList) == 0 {
			return fmt.Errorf("章节数据中没有找到图片")
		}

		fmt.Printf("找到 %d 张图片，scramble_seed: %d\n", len(ps.EpisodeData.PageList), ps.EpisodeData.ScrambleSeed)

		// 创建输出目录
		if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
			return fmt.Errorf("创建输出目录失败: %w", err)
		}

		// 顺序下载图片
		var successCount int
		var failedCount int

		fmt.Printf("正在下载第 %d/%d 页...\n", pageNum, len(ps.EpisodeData.PageList))

		// 下载图片
		imgData, err := ps.DownloadImage(imgURL, config.Client, config.Timeout)
		if err != nil {
			fmt.Printf("❌ 第 %d 页下载失败: %v\n", pageNum, err)
			failedCount++
			continue
		}

		// 处理图片（解扰）
		processedData, err := ps.ProcessImage(imgData, ps.EpisodeData.ScrambleSeed, config.TileCount)
		if err != nil {
			fmt.Printf("❌ 第 %d 页处理失败: %v\n", pageNum, err)
			failedCount++
			continue
		}

		// 保存图片文件
		filename := fmt.Sprintf("%03d.jpg", pageNum)
		filepath := filepath.Join(config.OutputDir, filename)
		if err := ps.SaveImage(processedData, filepath); err != nil {
			fmt.Printf("❌ 第 %d 页保存失败: %v\n", pageNum, err)
			failedCount++
			continue
		}

		successCount++
		fmt.Printf("✓ 第 %d 页下载完成: %s\n", pageNum, filename)

		// 添加短暂延迟，避免请求过快
		if pageNum < len(ps.EpisodeData.PageList) {
			time.Sleep(500 * time.Millisecond)
		}

		// 每个页面后再次检查
		if a.shouldStopDownload(sessionId) {
			log.Printf("[Backend] ❌ 页面处理后检测到停止")
			return nil
		}
	}

	// 发送完成
	if !a.isForceStop() {
		a.sendProgressSafely(DownloadProgress{
			Current: totalPages,
			Total:   totalPages,
			Title:   title,
			Status:  "completed",
		}, sessionId)
	}

	return nil
}

// 新增：带会话ID的停止检查
func (a *App) shouldStopDownload(sessionId int64) bool {
	a.downloadMutex.RLock()
	defer a.downloadMutex.RUnlock()

	// 检查强制停止
	if a.forceStop {
		return true
	}

	// 检查会话是否过期（防止旧会话继续）
	if a.downloadSessionId != sessionId {
		log.Printf("[Backend] ⚠️ 会话 %d 已过期，当前会话: %d", sessionId, a.downloadSessionId)
		return true
	}

	return false
}

// 新增：检查强制停止
func (a *App) isForceStop() bool {
	a.downloadMutex.RLock()
	defer a.downloadMutex.RUnlock()
	return a.forceStop
}

// 新增：设置强制停止
func (a *App) setForceStop() {
	a.downloadMutex.Lock()
	a.forceStop = true
	a.downloadMutex.Unlock()
}

func (a *App) CancelDownload() error {
	log.Println("[Backend] 🚨 收到强制取消请求")

	a.downloadMutex.Lock()

	// 记录取消时间
	a.lastCancelTime = time.Now()

	if !a.isDownloading {
		a.downloadMutex.Unlock()
		log.Println("[Backend] 没有活跃下载")
		return nil
	}

	// 设置强制停止标志
	a.forceStop = true
	log.Println("[Backend] ✅ 已设置强制停止标志")
	a.downloadMutex.Unlock()

	// 发送强停止信号
	a.sendForceStopSignals()

	// 强制清理
	a.forceCleanupAll()

	log.Println("[Backend] ✅ 强制取消完成")
	return nil
}

func (a *App) sendForceStopSignals() {
	// 清空通道
	a.clearAllChannels()

	// 发送多次停止信号确保接收
	for i := 0; i < 3; i++ {
		select {
		case a.downloadCancelChan <- struct{}{}:
			log.Println("[Backend] 发送取消信号")
		default:
		}

		select {
		case a.progressStopChan <- struct{}{}:
			log.Println("[Backend] 发送停止进度信号")
		default:
		}

		// 短暂延迟
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *App) clearAllChannels() {
	// 清空下载取消通道
	for {
		select {
		case <-a.downloadCancelChan:
			continue
		default:
		}
		break
	}

	// 清空进度停止通道
	for {
		select {
		case <-a.progressStopChan:
			continue
		default:
		}
		break
	}
}

func (a *App) forceCleanupAll() {
	a.downloadMutex.Lock()
	defer a.downloadMutex.Unlock()

	a.isDownloading = false
	a.forceStop = true

	// 清空所有通道
	a.clearAllChannels()

	// 停止所有事件监听器
	for name, stopFunc := range a.eventListeners {
		if stopFunc != nil {
			stopFunc()
			log.Printf("[Backend] 停止监听器: %s", name)
		}
	}
	a.eventListeners = make(map[string]func())
}

func (a *App) cleanupDownloadState() {
	a.downloadMutex.Lock()
	defer a.downloadMutex.Unlock()

	a.isDownloading = false
	a.forceStop = false
}

func (a *App) sendProgressSafely(progress DownloadProgress, sessionId int64) error {
	// 检查会话是否有效
	if a.shouldStopDownload(sessionId) {
		log.Printf("[Backend] ❌ 会话 %d 已停止，不发送进度", sessionId)
		return fmt.Errorf("会话已停止")
	}

	// 检查进度停止通道
	select {
	case <-a.progressStopChan:
		log.Println("[Backend] ❌ 收到停止信号，不发送进度")
		a.setForceStop()
		return fmt.Errorf("进度发送已停止")
	default:
	}

	// 检查上下文
	if a.ctx == nil {
		return fmt.Errorf("上下文无效")
	}

	// 发送进度
	log.Printf("[Backend] 📤 发送进度: %+v [会话:%d]", progress, sessionId)
	runtime.EventsEmit(a.ctx, "download-progress", progress)
	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr))
}

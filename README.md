# mg-downloader

这是一个简单的漫画下载器，基于wails+vue-ts。

## 支持的网站

· comic-days.com

· ourfeel.jp

· pocket.shonenmagazine.com

## 编译

本项目使用wails v2.11构建，请自己配置好wails v2.11

clone该项目到本地，在main.go里将frontend.rar展开。

调试模式编译：

```
wails dev
```

生产模式编译：

```
wails build
```

## 使用

首先你需要一个浏览器插件（这里推荐谷歌浏览器）：https://cookie-editor.com（cookie-editor）。然后登录网站。登录网页后，在首页获取cookie，导出成json。

然后在cookies文件夹内，cd是comic-days的cookie文件，ps是pocket shonenmagazine的cookie文件，选择对应的cookie文件，将json化的cookie粘贴到cookie文件里面，保存即可使用。

## 交流

本项目有且仅有一个qq交流群：1076094887。欢迎加入。一起探讨漫画或者技术，未来项目的第一消息将在群里公布。

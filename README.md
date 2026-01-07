# Telegram 后端监控机器人

一个用于监控 subconverter 与 SubConverter-Extended 后端服务状态的 Telegram 机器人，Go 实现，低内存占用。

## ✅ 功能特性
- 🚦 监控后端服务状态
- ✨ 自动识别 SubConverter-Extended / subconverter
- 🧭 支持多后端地址 (最多 20 个)
- 📦 显示版本信息 (Extended: Version/Build/Build Date)
- 🌐 支持中英文命令
- 🧰 详细的错误处理

## 🤖 机器人命令
- `/backend` - 检查后端状态 (英文)
- `/后端状态` 或发送 `后端状态` - 检查后端状态 (中文)

## 🐳 Docker Compose 部署

镜像由 GitHub Actions 自动发布到 Docker Hub，无需本地构建。

### 1. 获取 BOT_TOKEN
1) 打开 Telegram，搜索 `@BotFather`
2) 发送 `/newbot`，按提示设置名称与用户名
3) 获得机器人 Token（形如 `123456:ABC...`），妥善保存

### 2. 配置 docker-compose.yml
编辑 `docker-compose.yml`：
- `BOT_TOKEN`: 必填，填写 BotFather 给的 Token
- `BACKEND_URLS`: 可选，多个后端用逗号/空格分隔；可只写域名，程序会自动拼接 `/version`

示例：
```yaml
services:
  tg-bot:
    image: aethersailor/tg-backend-bot:latest
    environment:
      BOT_TOKEN: "YOUR_BOT_TOKEN"
      BACKEND_URLS: "api.asailor.org,legacy-api.asailor.org,example.com:25500"
```

### 3. 启动与日志
```bash
docker compose up -d

docker compose logs -f
```

## ☁️ Cloudflare Worker 部署 (Webhook)

说明：Worker 仅支持 webhook，请勿与 Docker 版本同时运行。Worker 部署不使用 GitHub Actions。

### 方案 A：一键部署
1) 点击下方按钮，按提示授权并创建 Worker
2) 部署完成后，进入 Cloudflare Dashboard -> Workers & Pages -> 你的 Worker
3) 打开 Settings -> Variables and Secrets，配置：
   - `BOT_TOKEN` (Secret，必填)
   - `WEBHOOK_SECRET` (Secret，可选，用于校验 Telegram webhook，可用随机字符串)
   - `BACKEND_URLS` (变量，可选，示例：`api.asailor.org,legacy-api.asailor.org,example.com:25500`)
4) 设置 Telegram Webhook（见下方命令）

一键部署按钮：

[![Deploy to Cloudflare Workers](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/Aethersailor/tg-backend-bot)

### 方案 B：手动部署（控制台）
1) 进入 Cloudflare Dashboard -> Workers & Pages -> Create -> Workers -> Start from scratch  
2) 设置 Worker 名称并点击 Deploy  
3) 进入 Worker 代码编辑页，将 `worker/src/index.js` 的内容粘贴覆盖，点击 Save and Deploy  
4) 打开 Settings -> Variables and Secrets，配置：  
   - `BOT_TOKEN` (Secret，必填)  
   - `WEBHOOK_SECRET` (Secret，可选，用于校验 Telegram webhook，可用随机字符串)  
   - `BACKEND_URLS` (变量，可选，示例：`api.asailor.org,example.com:25500`)  
5) 设置 Telegram Webhook（见下方命令）

### 设置 Telegram Webhook
```bash
# 若设置了 WEBHOOK_SECRET，建议带上 secret_token
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/setWebhook?url=<YOUR_WORKER_URL>&secret_token=<YOUR_WEBHOOK_SECRET>"

# 未设置 WEBHOOK_SECRET 时
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/setWebhook?url=<YOUR_WORKER_URL>"
```

## ⚙️ GitHub Actions 工作流 (维护者)
本仓库使用 GitHub Actions 自动构建并推送 Docker Hub 镜像 `aethersailor/tg-backend-bot:latest`。

需要在 GitHub 仓库 `Settings -> Secrets and variables -> Actions` 配置：
- `DOCKERHUB_USERNAME`：Docker Hub 用户名
- `DOCKERHUB_TOKEN`：Docker Hub Access Token

获取步骤简述：
- **Docker Hub Token**：Docker Hub -> Account Settings -> Security -> New Access Token

## 🐛 故障排除
- **容器没有日志**：`docker compose logs -f`
- **健康检查失败**：`docker exec -it tg-backend-bot /tg-backend-bot --healthcheck`
- **Webhook 无响应**：确认 webhook URL 可访问，并检查是否设置了正确的 `WEBHOOK_SECRET`

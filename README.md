# Apple Store Watcher

苹果商店到店取货监控。定时检查指定商品在指定 Apple Store 的库存，一旦可以到店取货就发送 HTTP 请求（Bark、ntfy、PushDeer、自建 webhook 等）。

已适配 2026 年 Apple Store 当前的接口，支持 **iPhone Duo**、iPhone 18 Pro、iPhone 17 等新商品，商品和门店列表都在运行时从官网读取，不需要再更新内置 JSON。

## 快速开始

```shell
go build -o apple-store-watcher .
cp config_demo.yaml config.yaml
./apple-store-watcher -c config.yaml
```

首次运行前，建议先确认门店 code 和商品 SKU：

```shell
# 查看门店（格式：城市 - 门店 - code）
./apple-store-watcher -store

# 查看当前 iPhone 商品（格式：名称 - SKU）
./apple-store-watcher -product
./apple-store-watcher -product | grep -i duo
```

### iPhone Duo SKU 速查

| 版本 | SKU |
| --- | --- |
| 夜空色 256GB | `MK2N4CH/A` |
| 夜空色 512GB | `MK2Q4CH/A` |
| 夜空色 1TB | `MK2U4CH/A` |
| 夜空色 2TB | `MK2W4CH/A` |
| 星光白色 256GB | `MK2M4CH/A` |
| 星光白色 512GB | `MK2P4CH/A` |
| 星光白色 1TB | `MK2T4CH/A` |
| 星光白色 2TB | `MK2V4CH/A` |

> 商品会随官网更新，建议运行 `./apple-store-watcher -product` 获取最新列表。

## 配置

```yaml
interval: 30s          # 轮询间隔
notify_interval: 30m   # 持续有货时的重复提醒间隔
timeout: 15s           # 单次请求超时

rules:
  - store: R389        # 上海 - 浦东
    trigger: "https://api.day.app/{your_bark_token}/iPhone%20Duo%20有货?url=https%3A%2F%2Fwww.apple.com.cn%2Fshop%2Fbag&sound=bell"
    sku:
      - MK2Q4CH/A      # iPhone Duo 512GB 夜空色
      - MK2P4CH/A      # iPhone Duo 512GB 星光白色
```

- `trigger` 接受任意 `http(s)` URL，程序会以 `POST` 方式请求（Bark 等支持这种方式）。
- `trigger` 支持三个占位符，发送前都会自动做 URL 编码：

  | 占位符 | 展开内容 | 示例 |
  | --- | --- | --- |
  | `{store}` | 门店 code | `R389` |
  | `{sku}` | 有货的 SKU，逗号分隔 | `MK2Q4CH%2FA` |
  | `{name}` | 有货的商品名，逗号分隔 | `iPhone+Duo+-+%E5%A4%9C%E7%A9%BA%E8%89%B2+-+512GB` |

  例如收到通知就能直接看到是哪个型号：

  ```yaml
  trigger: "https://api.day.app/token/有货了?store={store}&name={name}"
  ```

  `{name}` 需要商品名，程序会在启动时额外拉取一次商品列表；没用到 `{name}` 时不会产生这次请求。若商品名获取失败，会自动回退成 SKU，不影响监控。
- 也兼容旧版配置：顶层直接写规则列表（`- store: ...`），无需 `rules:`。

### 通知策略

- 无货 → 有货：立即发送通知。
- 持续有货：每隔 `notify_interval` 再提醒一次，避免刷屏。
- 有货 → 无货：记录日志并继续监控。

## 命令行参数

```text
-c string        配置文件路径（默认 config.yaml）
-store           打印当前门店列表
-product         打印当前商品列表
-family string   商品品类：iphone,ipad,watch,all（默认 iphone）
-once            只检查一次并打印结果，不发送通知
-v               输出详细日志
```

`-once` 适合配合 cron 使用：

```shell
./apple-store-watcher -c config.yaml -once
```

输出示例：

```text
R389 MK2Q4CH/A 无货 (有货门店: 0)
R389 MK2P4CH/A 无货 (有货门店: 0)
```

## 实现说明

本项目使用的都是 Apple Store 官网当前实际调用的接口：

| 用途 | 接口 |
| --- | --- |
| 门店列表 | `POST /api-www/graphql`（`StoreSearchByLocale`），失败时回退解析 `/retail/storelist` |
| 商品列表 | `/shop/buy-iphone` 等购买页中的 `PRODUCT_SELECTION_BOOTSTRAP` |
| 到店取货 | `GET /shop/sba/availability-message?fae=true&parts.N=...` |

旧版使用的 `rsp-web/store-list` 和 `shop/fulfillment-messages` 已经失效，因此本次改造不再依赖它们。

## 开发

```shell
go test ./...
go vet ./...
```

目录结构：

```text
main.go                     命令行入口
internal/apple/             Apple 官网接口客户端
internal/config/            YAML 配置解析（兼容旧格式）
internal/notify/            通知发送
internal/watcher/           轮询与通知策略
```

## 注意事项

- 请合理设置 `interval`，不建议低于 30 秒，避免给 Apple 服务造成压力。
- 商品和门店数据来自公开网页，Apple 调整页面结构后可能需要更新解析逻辑。
- 本项目仅用于个人库存提醒，请勿用于自动下单或商业用途。

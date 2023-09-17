# Apple Store Watcher

苹果商店到店取货监控。定时检测指定商品在指定门店的库存，如果可以到店取货，就发送Http请求。

配置格式如下：

```yaml
- store: R575
  trigger: "https://api.day.app/{your_bark_token}/JustBuyIt?url=https%3A%2F%2Fwww.apple.com.cn%2Fshop%2Fbag&sound=bell"
  sku:
    - MU2Q3CH/A
    - MU2P3CH/A
```

### 查看门店code

```shell
$./apple-store-watcher -store 
上海 - 七宝 - R705
上海 - 上海环贸 iapm  - R401
上海 - 五角场 - R581
上海 - 南京东路 - R359
上海 - 浦东 - R389
上海 - 环球港 - R683
上海 - 香港广场 - R390
......
```

> 可以使用grep自行过滤

### 查看商品code

```shell
$./apple-store-watcher -product 
iphone15promax - 原色钛金属 - 1tb - MU603CH/A
iphone15promax - 原色钛金属 - 256gb - MU2Q3CH/A
iphone15promax - 原色钛金属 - 512gb - MU2V3CH/A
iphone15promax - 白色钛金属 - 1tb - MU2Y3CH/A
iphone15promax - 白色钛金属 - 256gb - MU2P3CH/A
iphone15promax - 白色钛金属 - 512gb - MU2U3CH/A
iphone15promax - 蓝色钛金属 - 1tb - MU613CH/A
iphone15promax - 蓝色钛金属 - 256gb - MU2R3CH/A
iphone15promax - 蓝色钛金属 - 512gb - MU2W3CH/A
iphone15promax - 黑色钛金属 - 1tb - MU2X3CH/A
iphone15promax - 黑色钛金属 - 256gb - MU2N3CH/A
iphone15promax - 黑色钛金属 - 512gb - MU2T3CH/A
......
```

> 可以使用grep自行过滤


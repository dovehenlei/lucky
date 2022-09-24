# Lucky(大吉)
 
<!-- TOC -->
- [Lucky(大吉)](#)
  - [特性](#特性)
  - [一键安装](#一键安装)
  - [使用](#使用)
  - [Docker中使用](#docker中使用)
  - [转发规则格式](#转发规则格式)
  - [其它启动参数](#其它启动参数)
  - [后台界面](#后台界面)

  - [开发编译](#开发编译)
  - [使用注意与常见问题](#使用注意与常见问题)
<!-- /TOC -->


## 特性

- 这是一个自用的,目前主要运行在自己的主路由(小米ax6000)里面的程序.

    - 后端golang,前端vue3
    - 支持Windows、Linux系统，支持x86、ARM、MIPS、MIPSLE等架构

- 目前已经实现的功能有
    - 1.替代socat,主要用于公网IPv6 tcp/udp转 内网ipv4
        - 支持界面化(web后台)管理转发规则,单条转发规则支持设置多个转发端口,一键开关指定转发规则
        - 单条规则支持黑白名单安全模式切换,白名单模式可以让没有安全验证的内网服务端口稍微安全一丢丢暴露到公网
        - Web后台支持查看最新100条日志
        - 另有精简版不带后台,支持命令行快捷设置转发规则,有利于空间有限的嵌入式设备运行.(不再提供编译版本,如有需求可以自己编译)
    - 2.动态域名服务
        - 参考和部分代码来自 https://github.com/jeessy2/ddns-go
        - 在ddns-go的基础上主要改进/增加的功能有
            - 1.同时支持接入多个不同的DNS服务商
            - 2.支持http/https/socks5代理设置
            - 3.自定义(Callback)和Webhook支持自定义headers
            - 4.支持BasicAuth
            - 5.DDNS任务列表即可了解全部信息(包含错误信息),无需单独查看日志.
            - 6.调用DNS服务商接口更新域名信息前可以先通过DNS解析域名比较IP,减少对服务商接口调用.
            - 其它细节功能自己慢慢发现...
            - 没有文档,后台各处的提示信息已经足够多.
            - 支持的DNS服务商和DDNS-GO一样,有Alidns(阿里云),百度云,Cloudflare,Dnspod(腾讯云),华为云.自定义(Callback)内置有每步,No-IP,Dynv6,Dynu模版,一键填充,仅需修改相应用户密码或者token即可快速接入.
    - 3.Http反向代理
            - 支持HttpBasic认证  
            - 支持IP黑白名单
            - 支持UserAgent黑白名单
            - 日志记录最近访问情况
            - 一键开关子规则
            - 前端域名与后端地址 支持一对一,一对多(均衡负载),多对多(下一级反向代理)

- 将要实现的功能
    - 有建议可联系作者.



## 一键安装

- [一键安装详看这里](https://github.com/gdy666/lucky-files)


## 使用


- [百度网盘下载地址](https://pan.baidu.com/s/1NfumD9XjYU3OTeVmbu6vOQ?pwd=6666)
    百度网盘版本可能会更新比较频繁,
    

- 默认后台管理地址 http://<运行设备IP>:16601
  默认登录账号: 666
  默认登录密码: 666

- 常规使用请用 -c <配置文件路径> 指定配置文件的路由方式运行 , -p <后台端口> 可以指定后台管理端口
    ```bash
    #仅指定配置文件路径(如果配置文件不存在会自动创建),建议使用绝对路径
    lucky -c 666.conf
    #同时指定后台端口 8899
    lucky -c 666.conf -p 8899
    ```

- 命令行直接运行转发规则,注意后台无法编辑修改命令行启动的转发规则,主要用在不带后台的精简版
    ```bash
    #指定后台端口8899 
    lucky -p 8899 <转发规则1> <转发规则2> <转发规则3>...<<转发规则N>
    ```


## Docker中使用

- 不挂载主机目录, 删除容器同时会删除配置

  ```bash
  # host模式, 同时支持IPv4/IPv6, Liunx系统推荐
  docker run -d --name lucky --restart=always --net=host gdy666/lucky
  # 桥接模式, 只支持IPv4, Mac/Windows推荐,windows另外会有专用版本支持ipv6,待开发
  docker run -d --name lucky --restart=always -p 16601:16601 gdy666/lucky
  ```

- 在浏览器中打开`http://主机IP:16601`，修改你的配置，成功
- [可选] 挂载主机目录, 删除容器后配置不会丢失。可替换 `/root/luckyconf` 为主机目录, 配置文件为lucky.conf

  ```bash
  docker run -d --name lucky --restart=always --net=host -v /root/luckyconf:/goodluck gdy666/lucky
  ```



## 转发规则格式
    例子1
    tcp6@:22222to192.168.31.1:22
    监听 tcp6 类型的22222端口转发至192.168.31.1的22端口

    例子2
    udp@:1194to192.168.31.36
    监听 udp(同时包含udp4和udp6)类型的1194端口转发至192.168.31.36的相同端口(1194)

    例子3
    tcp6,udp6@:53to192.168.31.1:53
    监听 tcp6和udp6类型的53端口转发至192.168.31.1的53端口

    如果你还是没法理解格式,那么可以通过web管理后台添加转发规则后直接在规则列表中一键复制自动生成的命令行配置
    需要注意的是这种方式导入的规则不包含规则中的其它参数部分,命令行模式的规则只支持通过设置启动参数共用相同的额外参数(一般使用影响不大,不需要理会)

## 其它启动参数
    使用后台管理的用户不需要理会这部分内容

    -pcl <num>  
    全局代理数量限制(默认128),每个端口转发对应一个代理,这个参数主要是为了防止用户误写规则,生成过多代理造成程序奔溃或占用资源过多,一般不需要动. 

    -gpmc <num>
    全局最大连接数(默认10240),设计这个参数是为防止由于未知原因被人恶意高并发访问搞挂运行设备或程序,请根据需求调整.

    -smc <num>
    单个代理(端口)的最大连接数

    -ups <num>
    UDP包最大长度,默认1500,一般使用情景不需要理会,有特殊使用情景再自行调整,比如内网小包性能测试.

    -upm <bool>
    UDP代理性能模式开关,打开后,多核CPU环境下有利于改善UDP小包转发性能,默认已打开.

    -udpshort <bool>
    UDP short模式,如果需要用到dns转发打开这个开关有助于节省资源.






## 后台界面
![规则设置](./previews/relayruleset.png)
![规则列表](./previews/relayrules.png)
![](./previews/whitelistset.png)
![](./previews/whitelist.png)
#### 动态域名服务

![](./previews/ddnslist.png)


![](./previews/iphistroy.png)

![](./previews/webhookhistroy.png)

![](./previews/domainsync.png)

#### Http反向代理
![](./previews/reverseproxy.png)





#开发编译
    带后台版本编译

    ```bash
    go build -v -tags "adminweb nomsgpack" -ldflags="-s -w"
    ```

    不带后台版本
    ```bash
    go build -v -tags "nomsgpack" -ldflags="-s -w"
    ```






## 使用注意与常见问题
 - 如果在mips架构CPU下运行有问题请使用未压缩(UPS压缩版本),
 - 已知upx3.96版压缩后的程序在mipsle下可能无法运行,虽然已经更换了upx版本,暂时未再发现异常.

 - 不同于防火墙端口转发规则,不要设置没有用上的端口,会增加内存的使用.

 - 小米路由 ipv4 类型的80和443端口被占用,但只设置监听tcp6(ipv6)的80/443端口转发规则完全没问题.

 - 如果需要使用白名单模式,请根据自身需求打开外网访问后台管理页面开关.

 - 转发规则启用异常,端口转发没有生效时请登录后台查看日志.


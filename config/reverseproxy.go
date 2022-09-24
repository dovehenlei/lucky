package config

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdy666/lucky/thirdlib/gdylib/ginutils"
	"github.com/gdy666/lucky/thirdlib/gdylib/httputils"
	"github.com/gdy666/lucky/thirdlib/gdylib/logsbuffer"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

var reverseProxyLogsStore map[string]*logsbuffer.LogsBuffer
var reverseProxyLogsStoreMu sync.Mutex

var reverseProxyServerStore sync.Map
var reverseProxyServerStoreMu sync.Mutex

func init() {
	reverseProxyLogsStore = make(map[string]*logsbuffer.LogsBuffer)
}

func CreateReverseProxyLogbuffer(key string, buffSize int) *logsbuffer.LogsBuffer {
	reverseProxyLogsStoreMu.Lock()
	defer reverseProxyLogsStoreMu.Unlock()
	var buf *logsbuffer.LogsBuffer
	var ok bool
	if buf, ok = reverseProxyLogsStore[key]; !ok {
		buf = &logsbuffer.LogsBuffer{}
		buf.SetBufferSize(buffSize)
		reverseProxyLogsStore[key] = buf
	} else if buf.GetBufferSize() != buffSize {
		buf.SetBufferSize(buffSize)
	}

	return buf
}

// TidyReverseProxyCache 整理反向代理日志缓存
func TidyReverseProxyCache() {
	ruleList := GetReverseProxyRuleList()
	var keyListBuffer strings.Builder
	for _, rule := range ruleList {
		keyListBuffer.WriteString(rule.DefaultProxy.Key)
		keyListBuffer.WriteString(",")
		for _, sr := range rule.ProxyList {
			keyListBuffer.WriteString(sr.Key)
			keyListBuffer.WriteString(",")
		}
	}

	keyListStr := keyListBuffer.String()
	reverseProxyLogsStoreMu.Lock()
	defer reverseProxyLogsStoreMu.Unlock()

	var needDeleteKeys []string
	for k := range reverseProxyLogsStore {
		if !strings.Contains(keyListStr, k) {
			needDeleteKeys = append(needDeleteKeys, k)
		}
	}

	for i := range needDeleteKeys {
		delete(reverseProxyLogsStore, needDeleteKeys[i])
		reverseProxyServerStore.Delete(needDeleteKeys[i])
	}

}

type SubReverProxyRule struct {
	Key string `json:"Key"`

	initOnce       sync.Once
	Locations      []string    `json:"Locations"` //长度大于1时均衡负载
	locationMutex  *sync.Mutex `json:"-"`
	locationsCount int         `json:"-"`
	locationIndex  uint64      `json:"-"`

	EnableAccessLog            bool   `json:"EnableAccessLog"`            //开启日志
	LogLevel                   int    `json:"LogLevel"`                   //日志输出级别
	LogOutputToConsole         bool   `json:"LogOutputToConsole"`         //日志输出到终端
	AccessLogMaxNum            int    `json:"AccessLogMaxNum"`            //最大条数
	WebListShowLastLogMaxCount int    `json:"WebListShowLastLogMaxCount"` //前端列表显示最新日志最大条数
	RequestInfoLogFormat       string `json:"RequestInfoLogFormat"`       //请求信息在日志中的格式

	ForwardedByClientIP bool         `json:"ForwardedByClientIP"`
	TrustedCIDRsStrList []string     `json:"TrustedCIDRsStrList"`
	RemoteIPHeaders     []string     `json:"RemoteIPHeaders"` //识别客户端原始IP的Http请求头
	TrustedProxyCIDRs   []*net.IPNet `json:"-"`

	AddRemoteIPToHeader  bool   `json:"AddRemoteIPToHeader"` //追加客户端连接IP到指定Header
	AddRemoteIPHeaderKey string `json:"AddRemoteIPHeaderKey"`

	EnableBasicAuth bool   `json:"EnableBasicAuth"` //启用BasicAuth认证
	BasicAuthUser   string `json:"BasicAuthUser"`   //如果配置此参数，暴露出去的 HTTP 服务需要采用 Basic Auth 的鉴权才能访问
	BasicAuthPasswd string `json:"BasicAuthPasswd"` //结合 BasicAuthUser 使用

	SafeIPMode        string   `json:"SafeIPMode"`        //IP过滤模式 黑白名单
	SafeUserAgentMode string   `json:"SafeUserAgentMode"` //UserAgent 过滤模式 黑白名单
	UserAgentfilter   []string `json:"UserAgentfilter"`   //UserAgent 过滤内容

	CustomRobotTxt bool   `json:"CustomRobotTxt"`
	RobotTxt       string `json:"RobotTxt"`
	//------------------
	logsBuffer *logsbuffer.LogsBuffer
	logrus     *logrus.Logger
	logger     *log.Logger
}

type ReverseProxyRule struct {
	RuleName   string `json:"RuleName"`
	RuleKey    string `json:"RuleKey"`
	Enable     bool   `json:"Enable"`
	ListenIP   string `json:"ListenIP"`
	ListenPort int    `json:"ListenPort"`
	EnableTLS  bool   `json:"EnableTLS"`
	Network    string `json:"Network"`

	DefaultProxy struct {
		SubReverProxyRule
	} `json:"DefaultProxy"`

	ProxyList  []ReverseProxy `json:"ProxyList"`
	domainsMap *sync.Map
	initOnec   sync.Once
}

func (r *ReverseProxyRule) Init() {
	r.initOnec.Do(func() {
		r.initDomainsMap()

	})
}

func (r *SubReverProxyRule) Logf(level logrus.Level, c *gin.Context, format string, v ...any) {
	clientIP := r.ClientIP(c)
	remoteIP := c.RemoteIP()
	method := c.Request.Method
	host := c.Request.Host
	//hostname, hostport := httputils.SplitHostPort(c.Request.Host)
	url := c.Request.URL.String()
	//path := c.Request.URL.Path

	r.GetLogrus().WithFields(logrus.Fields{
		"ClientIP": clientIP,
		"RemoteIP": remoteIP,
		"Method":   method,
		"Host":     host,
		// "Hostname":  hostname,
		// "Hostport":  hostport,
		"URL": url,
		//"path":      path,
		"UserAgent": c.Request.UserAgent(),
	}).Logf(level, format, v...)
}

func (r *SubReverProxyRule) HandlerReverseProxy(remote *url.URL, path string, c *gin.Context) {

	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.Director = func(req *http.Request) {
		req.Header = c.Request.Header
		req.Host = remote.Host
		req.URL.Scheme = remote.Scheme
		req.URL.Host = remote.Host
		req.URL.Path = path
		if r.AddRemoteIPToHeader && r.AddRemoteIPHeaderKey != "" {
			cip := r.ClientIP(c)
			req.Header.Add(r.AddRemoteIPHeaderKey, cip)
		}
	}
	proxy.ErrorLog = r.GetLogger()
	proxy.ServeHTTP(c.Writer, c.Request)

}

func (r *SubReverProxyRule) PrintfToConsole(entry *logrus.Entry) error {
	if !r.LogOutputToConsole {
		return nil
	}

	s, _ := entry.String()
	log.Print(s)
	return nil
}

func (r *SubReverProxyRule) GetLogrus() *logrus.Logger {
	if r.logrus == nil {
		r.logrus = logrus.New()
		r.logrus.SetLevel(logrus.Level(r.LogLevel))
		r.GetLogsBuffer().SetFireCallback(r.PrintfToConsole)
		r.logrus.SetOutput(r.GetLogsBuffer())
		r.logrus.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat:   "2006-01-02 15:04:05",
			DisableTimestamp:  true,
			DisableHTMLEscape: true,
			DataKey:           "ExtInfo",
		})

		r.logrus.AddHook(r.GetLogsBuffer())

	}
	return r.logrus
}

func (r *SubReverProxyRule) GetLogger() *log.Logger {
	if r.logger == nil {
		r.logger = log.New(r.GetLogsBuffer(), "", log.LstdFlags)
	}
	return r.logger
}

func (r *SubReverProxyRule) GetLogsBuffer() *logsbuffer.LogsBuffer {
	if r.logsBuffer == nil {
		r.logsBuffer = CreateReverseProxyLogbuffer(r.Key, r.AccessLogMaxNum)
	}
	return r.logsBuffer
}

func (r *SubReverProxyRule) checkupClientIP(ip string) bool {
	return SafeCheck(r.SafeIPMode, ip)
}

func (r *SubReverProxyRule) checkupUserAgent(ua string) bool {

	isContains := false
	for _, c := range r.UserAgentfilter {
		if strings.Contains(ua, c) {
			isContains = true
			break
		}
	}

	switch r.SafeUserAgentMode {
	case "whitelist":
		return isContains
	case "blacklist":
		return !isContains
	default:
		return false
	}
}

func (r *ReverseProxyRule) ReverseProxyHandler(c *gin.Context) {
	path := c.Param("proxyPath")
	hostName, _ := httputils.SplitHostPort(c.Request.Host)
	rule, ok := r.GetSubRuleByDomain(hostName)

	var subRule *SubReverProxyRule = nil
	if ok && rule.Enable {
		subRule = &rule.SubReverProxyRule
	} else {
		subRule = &r.DefaultProxy.SubReverProxyRule
	}

	if !subRule.checkupClientIP(subRule.ClientIP(c)) { //IP检查
		subRule.Logf(logrus.WarnLevel, c, "IP[%s]禁止访问,当前Ip检查模式[%s]", subRule.ClientIP(c), subRule.SafeIPMode)
		c.Abort()
		return
	}

	if !subRule.checkupUserAgent(c.Request.UserAgent()) {
		subRule.Logf(logrus.WarnLevel, c, "IP[%s]UA[%s]禁止访问,当前UA检查模式[%s]", subRule.ClientIP(c), c.Request.UserAgent(), subRule.SafeUserAgentMode)
		c.Abort()
		return
	}

	if !subRule.BasicAuthHandler(c) {
		subRule.Logf(logrus.WarnLevel, c, "BasicAuth认证不通过")
		c.Abort()
		return
	}

	if subRule.CustomRobotTxt && c.Request.RequestURI == "/robots.txt" {
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			status := http.StatusOK
			if c.Request.Method != "OPTIONS" {
				status = http.StatusMethodNotAllowed
			}
			c.Header("Allow", "GET,HEAD,OPTIONS")
			c.AbortWithStatus(status)
			return
		}
		c.Data(http.StatusOK, "text/plain", []byte(subRule.RobotTxt))
		subRule.Logf(logrus.InfoLevel, c, "触发自定义robots.txt")
		return
	}

	location := subRule.GetLocation()
	if location == "" && subRule.Key == r.RuleKey {
		subRule.Logf(logrus.InfoLevel, c, "域名[%s]没有对应后端地址,默认后端地址没有设置", hostName)
		c.Abort()
		return
	}

	if subRule.Key == r.RuleKey {
		subRule.Logf(logrus.InfoLevel, c, "[%s] 指向默认后端地址[%s%s]", hostName, location, c.Request.URL.String())
	} else {
		subRule.Logf(logrus.InfoLevel, c, "[%s] 指向后端地址[%s%s]", hostName, location, c.Request.URL.String())
	}

	remote, err := url.Parse(location)
	if err != nil {
		subRule.Logf(logrus.ErrorLevel, c, "后端地址转换出错:%s", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"ret": 1, "msg": fmt.Sprintf("后端地址[%s] 转换出错:%s", location, err.Error())})
		return
	}
	subRule.HandlerReverseProxy(remote, path, c)

}

func (r *ReverseProxyRule) GetSubRuleByDomain(domain string) (*ReverseProxy, bool) {
	val, ok := r.domainsMap.Load(domain)
	if !ok {
		return nil, false
	}
	return val.(*ReverseProxy), true
}

type ReverseProxy struct {
	SubReverProxyRule
	Enable  bool     `json:"Enable"`
	Remark  string   `json:"Remark"`
	Domains []string `json:"Domains"` //自定义域名

}

func GetSubRuleByKey(ruleKey, proxyKey string) *SubReverProxyRule {
	//rule := getSubRuleByKey()

	rule := GetReverseProxyRuleByKey(ruleKey)
	if rule == nil {
		return nil
	}

	//fmt.Printf("FFF ruleKey:%s proxyKey:%s\n", ruleKey, proxyKey)

	if proxyKey == "default" {

		return &rule.DefaultProxy.SubReverProxyRule
	}

	for i := range rule.ProxyList {
		if rule.ProxyList[i].Key == proxyKey {
			return &rule.ProxyList[i].SubReverProxyRule
		}
	}
	return nil
}

func (r *ReverseProxyRule) GetServer() *http.Server {
	s, loaded := reverseProxyServerStore.Load(r.RuleKey)
	if !loaded {
		return nil
	}
	return s.(*http.Server)
}

func (r *ReverseProxyRule) SetServer(s *http.Server) {
	if s == nil {
		reverseProxyServerStore.Delete(r.RuleKey)
		return
	}
	reverseProxyServerStore.Store(r.RuleKey, s)
}

func (r *ReverseProxyRule) ServerStart() error {
	// r.smu.Lock()
	// defer r.smu.Unlock()
	reverseProxyServerStoreMu.Lock()
	defer reverseProxyServerStoreMu.Unlock()
	server := r.GetServer()

	if server != nil {
		return fmt.Errorf("RuleServer[%s]已经启动,请勿重复启动", r.Addr())
	}
	ginR := gin.New()

	ginR.Any("/*proxyPath", r.ReverseProxyHandler)
	server = &http.Server{
		Addr:    r.Addr(),
		Handler: ginR,
	}

	ln, err := net.Listen(r.Network, r.Addr())
	if err != nil {
		return err
	}

	var serveResult error

	go func() {
		serveResult = server.Serve(ln)
	}()

	<-time.After(time.Millisecond * 300)

	defer func() {
		if serveResult == nil {
			//setPreReverseProxyHttpServer(r.RuleKey, r.server)
			r.SetServer(server)
		}
	}()

	return serveResult

}

func (r *ReverseProxyRule) ServerStop() {
	reverseProxyServerStoreMu.Lock()
	defer reverseProxyServerStoreMu.Unlock()
	server := r.GetServer()
	if server == nil {
		return
	}
	server.Close()
	r.SetServer(nil)

}

func (r *ReverseProxyRule) initDomainsMap() error {
	r.domainsMap = &sync.Map{}
	for i := range r.ProxyList {
		for j := range r.ProxyList[i].Domains {
			_, loaded := r.domainsMap.LoadOrStore(r.ProxyList[i].Domains[j], &r.ProxyList[i])
			if loaded {
				return fmt.Errorf("前端域名[%s]冲突", r.ProxyList[i].Domains[j])
			}
		}
	}
	return nil
}

func (r *SubReverProxyRule) initOnceExec() {
	r.initOnce.Do(func() {
		r.locationsCount = len(r.Locations)
		r.InitTrustedProxyCIDRs()
		r.locationMutex = &sync.Mutex{}
	})
}

func (r *SubReverProxyRule) GetLocation() string {
	r.initOnceExec()
	r.locationMutex.Lock()
	defer func() {
		r.locationIndex++
		r.locationMutex.Unlock()
	}()

	if r.locationsCount == 0 {
		return ""
	}

	return r.Locations[r.locationIndex%uint64(r.locationsCount)]
}

func (r *SubReverProxyRule) BasicAuthHandler(c *gin.Context) bool {
	if !r.EnableBasicAuth || r.BasicAuthUser == "" {
		return true
	}

	realm := "Basic realm=" + strconv.Quote("Authorization Required")
	pairs := ginutils.ProcessAccounts(gin.Accounts{r.BasicAuthUser: r.BasicAuthPasswd})
	user, found := pairs.SearchCredential(c.GetHeader("Authorization"))
	if !found {
		// Credentials doesn't match, we return 401 and abort handlers chain.
		c.Header("WWW-Authenticate", realm)
		c.AbortWithStatus(http.StatusUnauthorized)
		return false
	}
	c.Set("user", user)
	return true
}

func (r *SubReverProxyRule) InitTrustedProxyCIDRs() error {
	var res []*net.IPNet
	for i := range r.TrustedCIDRsStrList {
		if strings.TrimSpace(r.TrustedCIDRsStrList[i]) == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(r.TrustedCIDRsStrList[i])
		if err != nil {
			return fmt.Errorf("[%s]网段格式有误", r.TrustedCIDRsStrList[i])
		}
		res = append(res, cidr)
	}
	r.TrustedProxyCIDRs = res
	return nil
}

func (r *SubReverProxyRule) ClientIP(c *gin.Context) string {
	remoteIP := net.ParseIP(c.RemoteIP())
	if remoteIP == nil {
		return ""
	}

	trusted := r.isTrustedProxy(remoteIP)

	if trusted && r.ForwardedByClientIP && r.RemoteIPHeaders != nil {
		for _, headerName := range r.RemoteIPHeaders {
			ip, valid := r.validateHeader(c.Request.Header.Get(headerName))
			if valid {
				return ip
			}
		}
	}

	return remoteIP.String()
}

func (r *SubReverProxyRule) validateHeader(header string) (clientIP string, valid bool) {
	if header == "" {
		return "", false
	}
	items := strings.Split(header, ",")
	for i := len(items) - 1; i >= 0; i-- {
		ipStr := strings.TrimSpace(items[i])
		ip := net.ParseIP(ipStr)
		if ip == nil {
			break
		}

		if (i == 0) || (!r.isTrustedProxy(ip)) {
			return ipStr, true
		}
	}
	return "", false
}

func (r *SubReverProxyRule) isTrustedProxy(ip net.IP) bool {
	r.initOnceExec()

	if r.TrustedProxyCIDRs == nil {
		return false
	}
	for _, cidr := range r.TrustedProxyCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func (r *ReverseProxyRule) Addr() string {
	return fmt.Sprintf("%s:%d", r.ListenIP, r.ListenPort)
}

type LogItem struct {
	ProxyKey   string
	ClientIP   string
	LogContent string
	LogTime    string
}

// 2006-01-02 15:04:05
func ReverseProxyLogConvert(lg *logsbuffer.LogItem) any {
	l := LogItem{
		LogContent: lg.Content,
		LogTime:    time.Unix(lg.Timestamp/int64(time.Second), 0).Format("2006-01-02 15:04:05")}
	return l
}

func (r *ReverseProxyRule) GetLastLogs() map[string][]any {
	res := make(map[string][]any)
	res["default"] = r.DefaultProxy.GetLogsBuffer().GetLastLogs(ReverseProxyLogConvert, r.DefaultProxy.WebListShowLastLogMaxCount)

	for i := range r.ProxyList {
		res[r.ProxyList[i].Key] = r.ProxyList[i].GetLogsBuffer().GetLastLogs(
			ReverseProxyLogConvert, r.ProxyList[i].WebListShowLastLogMaxCount)
	}
	return res
}

//------------------------------------------------------------

func GetReverseProxyRuleList() []*ReverseProxyRule {
	programConfigureMutex.RLock()
	defer programConfigureMutex.RUnlock()

	var resList []*ReverseProxyRule

	for i := range programConfigure.ReverseProxyRuleList {
		programConfigure.ReverseProxyRuleList[i].Init()
		rule := programConfigure.ReverseProxyRuleList[i]
		resList = append(resList, &rule)
	}
	return resList
}

func GetReverseProxyRuleByKey(ruleKey string) *ReverseProxyRule {
	programConfigureMutex.Lock()
	defer programConfigureMutex.Unlock()
	ruleIndex := -1

	for i := range programConfigure.ReverseProxyRuleList {
		if programConfigure.ReverseProxyRuleList[i].RuleKey == ruleKey {
			ruleIndex = i
			break
		}
	}
	if ruleIndex == -1 {
		return nil
	}
	res := programConfigure.ReverseProxyRuleList[ruleIndex]
	return &res
}

func ReverseProxyRuleListAdd(rule *ReverseProxyRule) error {
	programConfigureMutex.Lock()
	defer programConfigureMutex.Unlock()

	programConfigure.ReverseProxyRuleList = append(programConfigure.ReverseProxyRuleList, *rule)
	return Save()
}

func ReverseProxyRuleListDelete(ruleKey string) error {
	programConfigureMutex.Lock()
	defer programConfigureMutex.Unlock()

	ruleIndex := -1

	for i := range programConfigure.ReverseProxyRuleList {
		if programConfigure.ReverseProxyRuleList[i].RuleKey == ruleKey {
			ruleIndex = i
			break
		}
	}

	if ruleIndex == -1 {
		return fmt.Errorf("找不到需要删除的DDNS任务")
	}

	programConfigure.ReverseProxyRuleList = DeleteReverseProxyRuleListlice(programConfigure.ReverseProxyRuleList, ruleIndex)
	return Save()
}

func EnableReverseProxyRuleByKey(ruleKey string, enable bool) error {
	programConfigureMutex.Lock()
	defer programConfigureMutex.Unlock()
	ruleIndex := -1

	for i := range programConfigure.DDNSTaskList {
		if programConfigure.ReverseProxyRuleList[i].RuleKey == ruleKey {
			ruleIndex = i
			break
		}
	}
	if ruleIndex == -1 {
		return fmt.Errorf("开关反向代理规则失败,ruleKey %s 未找到", ruleKey)
	}
	programConfigure.ReverseProxyRuleList[ruleIndex].Enable = enable

	return Save()
}

func EnableReverseProxySubRule(ruleKey, proxyKey string, enable bool) error {
	programConfigureMutex.Lock()
	defer programConfigureMutex.Unlock()
	ruleIndex := -1

	for i := range programConfigure.DDNSTaskList {
		if programConfigure.ReverseProxyRuleList[i].RuleKey == ruleKey {
			ruleIndex = i
			break
		}
	}
	if ruleIndex == -1 {
		return fmt.Errorf("开关反向代理子规则失败,ruleKey %s 未找到", ruleKey)
	}

	proxyIndex := -1
	for i := range programConfigure.ReverseProxyRuleList[ruleIndex].ProxyList {
		if programConfigure.ReverseProxyRuleList[ruleIndex].ProxyList[i].Key == proxyKey {
			proxyIndex = i
			break
		}
	}

	if proxyIndex == -1 {
		return fmt.Errorf("开关反向代理子规则失败,proxyKey %s 未找到", proxyKey)
	}

	programConfigure.ReverseProxyRuleList[ruleIndex].ProxyList[proxyIndex].Enable = enable

	return Save()

}

func UpdateReverseProxyRulet(rule ReverseProxyRule) error {
	programConfigureMutex.Lock()
	defer programConfigureMutex.Unlock()
	ruleIndex := -1

	for i := range programConfigure.DDNSTaskList {
		if programConfigure.ReverseProxyRuleList[i].RuleKey == rule.RuleKey {
			ruleIndex = i
			break
		}
	}

	if ruleIndex == -1 {
		return fmt.Errorf("找不到需要更新的反向代理规则")
	}

	//	rule.RuleKey = programConfigure.ReverseProxyRuleList[ruleIndex].RuleKey
	programConfigure.ReverseProxyRuleList[ruleIndex] = rule

	return Save()
}

func DeleteReverseProxyRuleListlice(a []ReverseProxyRule, deleteIndex int) []ReverseProxyRule {
	j := 0
	for i := range a {
		if i != deleteIndex {
			a[j] = a[i]
			j++
		}
	}
	return a[:j]
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

type VoteStatus struct {
	LineId   int
	Sno      int
	Sname    string
	IsVoted  string
	VoteTime string
}

func (v VoteStatus) String() string {
	return fmt.Sprintf("行号: %v 学号: %v 姓名:%v 是否核酸:%v 核酸时间:%v \n", v.LineId, v.Sno, v.Sname, v.IsVoted, v.VoteTime)
}

var redisDb *redis.Client
var m = make(map[string]interface{})
var zone *time.Location
var voteTime = make(map[string]string)

func Init() {
	err := initRedis()
	if err != nil {
		panic(err)
		return
	}
	logrus.Info("连接redis成功")

	err = DataInit()
	if err != nil {
		panic(err)
		return
	}
	logrus.Info("数据初始化成功")

	voteTime = map[string]string{}
	logrus.Info("投票时间初始化成功")
}
func main() {
	logrus.SetFormatter(&logrus.TextFormatter{})
	logHandler, err := os.Create("debug.log")
	if err != nil {
		logrus.Error("debugLog init failed :", err)
		panic(err.Error())
	}
	logrus.SetOutput(logHandler)

	Init()

	go func() {
		for {
			now := time.Now().In(zone)
			if now.Hour() == 23 && now.Minute() == 59 {
				SaveAndEmail()
				Init()
				time.Sleep(60 * time.Second)
				logrus.Info("新一轮循环 数据格式化完成")
			}
		}
	}()

	engine := gin.Default()
	engine.LoadHTMLGlob("template/*")
	engine.POST("/fuckVote", fuckVote)
	engine.GET("/Vote", vote)
	engine.GET("/statistic", func(context *gin.Context) {
		SaveAndEmail()
	})
	err = engine.Run(":9999")
	if err != nil {
		panic(err.Error())
	}
}

func SaveAndEmail() {
	idx := 1
	// 加载配置文件，登录至邮箱
	config := LoadConfig("./config.json")
	buffer := bytes.Buffer{}

	snameByScore, err := redisDb.ZRangeByScore("ZsStudent", redis.ZRangeBy{
		Min:    "2020170229",
		Max:    "2020170285",
		Offset: 0,
		Count:  0,
	}).Result()
	if err != nil {
		logrus.Info("redis zRangeByScore 获取失败 请联系管理员")
		return
	}

	for i := 0; i < len(snameByScore); i++ {
		//"2020170281"
		itoa := strconv.Itoa(m[snameByScore[i]].(int))
		//0
		bit, err := redisDb.HGet("isVoted", itoa).Result()
		if err != nil {
			logrus.Info("redis isVoted 获取失败 请联系管理员")
			return
		}
		temp := "未核酸🧬"
		if bit != "0" {
			temp = "已核酸🎉🎉🎉"
			continue
		}
		status := VoteStatus{
			LineId:   idx,
			Sno:      m[snameByScore[i]].(int),
			Sname:    snameByScore[i],
			IsVoted:  temp,
			VoteTime: voteTime[snameByScore[i]],
		}

		buffer.WriteString(status.String())
		idx++
	}

	title := fmt.Sprintf("%s-核酸检测统计", time.Now().In(zone).Format("2006-01-02"))
	msg := &Msg{
		//Tmail:   config.Email,
		Tmail:   "1018814650@qq.com",
		Title:   title,
		Content: buffer.String(),
	}

	if config.Email != "" && title != "" && buffer.String() != "" {
		SendMail(config, msg)
	} else {
		panic("to,title,content can't be null!")
	}
}

func fuckVote(context *gin.Context) {
	sname := context.PostForm("name")
	sno, ok := m[sname].(int)
	if !ok {
		context.JSON(http.StatusBadRequest, "该姓名不存在 请回退重新输入")
		context.Error(errors.New("该姓名不存在 请重新输入"))
		logrus.Error(errors.New("该姓名不存在 请重新输入"))
		return
	}
	itoa := strconv.Itoa(sno)
	_, err := redisDb.HSet("isVoted", itoa, 1).Result()
	if err != nil {
		context.JSON(http.StatusInternalServerError, "投票失败 redis Set isVoted failed 请联系管理员")
		context.Error(errors.New("投票失败 redis Set isVoted failed 请联系管理员"))
		logrus.Error(errors.New("投票失败 redis Set isVoted failed 请联系管理员"))
		return
	}
	voteTime[sname] = time.Now().In(zone).Format("2006-01-02 15:04:05")
	context.Redirect(http.StatusFound, "/Vote")
}

func SendMail(config *Config, ms *Msg) {
	auth := smtp.PlainAuth("", config.Email, config.Password, config.Mailserver)
	to := []string{ms.Tmail, "1018814650@qq.com"} //接收用户
	user := config.Email
	nickname := config.Name

	subject := ms.Title
	content_type := "Content-Type: text/plain; charset=UTF-8"
	body := ms.Content
	msg := "To:" + strings.Join(to, ",") + "\r\nFrom: "
	msg += nickname + "<" + user + ">\r\nSubject: " + subject
	msg += "\r\n" + content_type + "\r\n\r\n" + body

	server := func(serverName, port string) string {
		var buffer bytes.Buffer
		buffer.WriteString(serverName)
		buffer.WriteString(":")
		buffer.WriteString(port)
		return buffer.String()
	}(config.Mailserver, config.Port)

	// 发送邮件
	err := smtp.SendMail(server, auth, user, to, []byte(msg))
	if err != nil {
		logrus.Errorf("send mail error:%v\n", err)
	}
	logrus.Info(msg, "\n")
}

func vote(context *gin.Context) {
	snameByScore, err := redisDb.ZRangeByScore("ZsStudent", redis.ZRangeBy{
		Min:    "2020170229",
		Max:    "2020170285",
		Offset: 0,
		Count:  0,
	}).Result()
	if err != nil {
		context.JSON(http.StatusInternalServerError, "redis zRangeByScore 获取失败 请联系管理员")
		context.Error(errors.New("redis zRangeByScore 获取失败 请联系管理员"))
		logrus.Error(errors.New("redis zRangeByScore 获取失败 请联系管理员"))
		return
	}

	VotedResult := make([]VoteStatus, 0, len(snameByScore))
	for i := 0; i < len(snameByScore); i++ {
		//"2020170281"
		itoa := strconv.Itoa(m[snameByScore[i]].(int))
		//0
		bit, err := redisDb.HGet("isVoted", itoa).Result()
		if err != nil {
			context.JSON(http.StatusInternalServerError, "redis isVoted 获取失败 请联系管理员")
			context.Error(errors.New("redis isVoted 获取失败 请联系管理员"))
			logrus.Error(errors.New("redis isVoted 获取失败 请联系管理员"))
			return
		}
		temp := "未核酸🧬"
		if bit != "0" {
			temp = "已核酸🎉🎉🎉"
		}
		VotedResult = append(VotedResult, VoteStatus{
			LineId:   i + 1,
			Sno:      m[snameByScore[i]].(int),
			Sname:    snameByScore[i],
			IsVoted:  temp,
			VoteTime: voteTime[snameByScore[i]],
		})
	}
	context.HTML(http.StatusOK, "vote.html", VotedResult)
}

func DataInit() (err error) {
	//set hStudent filed["林健树"] 2020170281 ["江文涛"] 2020170282
	err = redisDb.HMSet("hStudent", m).Err()
	if err != nil {
		logrus.Error(err)
		return
	}

	//zSort score 2020170281 member 林健树
	var zs []redis.Z
	for key, value := range m {
		zs = append(zs, redis.Z{
			Score:  float64(value.(int)),
			Member: key,
		})
	}

	err = redisDb.ZAdd("ZsStudent", zs...).Err()
	if err != nil {
		logrus.Error(err)
		return
	}

	isVotedMap := make(map[string]interface{})
	for _, value := range m {
		itoa := strconv.Itoa(value.(int))
		isVotedMap[itoa] = 0
	}

	//set isVoted field["2020170281"] 0
	err = redisDb.HMSet("isVoted", isVotedMap).Err()
	if err != nil {
		logrus.Error(err)
		return
	}

	//固定时区
	zone = time.FixedZone("CST", 8*3600)
	return
}
func initRedis() (err error) {
	redisDb = redis.NewClient(&redis.Options{
		//Addr:     "127.0.0.1:6379",
		Addr:     "127.0.0.1:49153",
		Password: "123456",
		DB:       0,
	})

	_, err = redisDb.Ping().Result()
	if err != nil {
		logrus.Error(err)
		return err
	}

	m["杜勇敢"] = 2020170229
	m["孟凡森"] = 2020170230
	m["李文博"] = 2020170231
	m["杨江帆"] = 2020170232
	m["边尚琪"] = 2020170233
	m["王雷"] = 2020170234
	m["杨庚"] = 2020170235
	m["涂启添"] = 2020170236
	m["冯也"] = 2020170237
	m["窦浩天"] = 2020170238
	m["权家友"] = 2020170239
	m["贵兴锋"] = 2020170240
	m["赵欢"] = 2020170241
	m["孙一鸣"] = 2020170242
	m["杜志杰"] = 2020170243
	m["帅深龙"] = 2020170244
	m["徐宏坤"] = 2020170245
	m["陈鑫"] = 2020170246
	m["鹿牧野"] = 2020170247
	m["周帆"] = 2020170248
	m["虢锐"] = 2020170249
	m["尹群群"] = 2020170250
	m["白杨"] = 2020170251
	m["孟丽"] = 2020170252
	m["陈瀚"] = 2020170253
	m["李润"] = 2020170254
	m["李掌珠"] = 2020170255
	m["杨子健"] = 2020170256
	m["李晓龙"] = 2020170257
	m["赵麟寒"] = 2020170258
	m["蒋晓明"] = 2020170259
	m["冯家乐"] = 2020170260
	m["石行"] = 2020170261
	m["赵仁陈"] = 2020170262
	m["邓峰"] = 2020170263
	m["王艺超"] = 2020170264
	m["赵智健"] = 2020170265
	m["韩大力"] = 2020170266
	m["陆国庆"] = 2020170267
	m["陈涛"] = 2020170268
	m["周紫剑"] = 2020170269
	m["刘亦丰"] = 2020170270
	m["姜晨皓"] = 2020170271
	m["丁颖"] = 2020170272
	m["黄以豪"] = 2020170273
	m["雷俊"] = 2020170274
	m["贺竞娇"] = 2020170275
	m["范永正"] = 2020170276
	m["肖锴"] = 2020170277
	m["张子豪"] = 2020170278
	m["钱坤"] = 2020170279
	m["孙凯"] = 2020170280
	m["林健树"] = 2020170281
	m["江文涛"] = 2020170282
	m["李继恩"] = 2020170283
	m["牛谊博"] = 2020170284
	m["赵梦梓"] = 2020170285
	return nil
}

type Config struct {
	Email      string `json:"email"`      //账号
	Name       string `json:"name"`       //发送者名字
	Password   string `json:"password"`   //邮箱授权码
	Mailserver string `json:"mailserver"` //邮件服务器
	Port       string `json:"port"`       //服务器端口
}

// LoadConfig 加载配置文件
func LoadConfig(configPath string) (config *Config) {
	// 读取配置文件
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		logrus.Error(err)
	}
	// 初始化用户信息
	config = &Config{}
	err = json.Unmarshal(data, &config)
	if err != nil {
		logrus.Error(err)
	}

	return config
}

// Msg 发送邮件信息
type Msg struct {
	Tmail   string
	Title   string
	Content string
}

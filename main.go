package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/s3"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mssola/user_agent"
)

const InsertStmt = "insert into s3_logs values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)"
const IpApiEndpoint = "http://ip-api.com/json/"

var internalUA = map[string]bool{
	"aws-internal/3":                     true,
	"Boto/2.9.9 (win32)":                 true,
	"S3Console/0.4":                      true,
	"Boto/2.38.0 Python/3.4.3 Windows/8": true,
	"Go-http-client/1.1":                 true,
}

type Configuration struct {
	AwsBucketName      string `json:"AWS_BUCKET_NAME"`
	AwsAccessKeyID     string `json:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey string `json:"AWS_SECRET_ACCESS_KEY"`
	AwsRegion          string `json:"AWS_REGION"`
	AddGeolocation     string `json:"AddGeolocation"`
	DeleteLogsFromS3   string `json:"DeleteLogsFromS3"`
	DeleteInternalLogs string `json:"DeleteInternalLogs"`
	DBLocation         string `json:"DBLocation"`
	ReportLocation     string `~json:"ReportLocation"`
}

type Log struct {
	BucketOwner             string
	Bucket                  string
	Time                    string
	RemoteIP                string
	RemoteCity              string
	RemoteCountry           string
	RemoteLat               float32
	RemoteLng               float32
	Requester               string
	RequestID               string
	Operation               string
	Key                     string
	RequestURI              string
	HTTPstatus              string
	ErrorCode               string
	BytesSent               int
	ObjectSize              int
	TotalTime               int
	TurnAroundTime          int
	Referrer                string
	UserAgent               string
	UserAgentBrowser        string
	UserAgentBrowserVersion string
	UserAgentPlatform       string
	UserAgentMobile         int
	UserAgentBot            int
	UserAgentOS             string
	UserAgentEngine         string
	UserAgentEngineVersion  string
	VersionId               string
}

type IpApiResponse struct {
	As          string  `json:"as"`
	City        string  `json:"city"`
	Country     string  `json:"country"`
	Countrycode string  `json:"countryCode"`
	Isp         string  `json:"isp"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Org         string  `json:"org"`
	Query       string  `json:"query"`
	Region      string  `json:"region"`
	Regionname  string  `json:"regionName"`
	Status      string  `json:"status"`
	Timezone    string  `json:"timezone"`
	Zip         string  `json:"zip"`
}

func readConfiguration() Configuration {
	file, err1 := os.Open("conf.json")
	if err1 != nil {
		fmt.Println("error:", err1)
	}
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err2 := decoder.Decode(&configuration)
	if err2 != nil {
		fmt.Println("error:", err2)
	}
	return configuration
}

func getGeolocation(ip string) IpApiResponse {

	resp, err := http.Get(string(IpApiEndpoint) + ip)
	if err != nil {
		log.Println(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
	}
	var data IpApiResponse
	json.Unmarshal(body, &data)

	data.Country = strings.Title(strings.ToLower(data.Country))
	data.City = strings.Title(strings.ToLower(data.City))
	time.Sleep(500 * time.Millisecond)
	return data

}

func newLog(reString []string) *Log {
	logline := new(Log)
	logline.BucketOwner = reString[1]
	logline.Bucket = reString[2]
	tmp, _ := time.Parse("02/Jan/2006:15:04:05 -0700", reString[3])
	logline.Time = tmp.Format("2006-01-02 15:04:05.000000")
	logline.RemoteIP = reString[4]
	logline.Requester = reString[5]
	logline.RequestID = reString[6]
	logline.Operation = reString[7]
	logline.Key = reString[8]
	logline.RequestURI = reString[9]
	logline.HTTPstatus = reString[10]
	logline.ErrorCode = reString[11]
	logline.BytesSent, _ = strconv.Atoi(reString[12])

	logline.ObjectSize, _ = strconv.Atoi(reString[13])
	logline.TotalTime, _ = strconv.Atoi(reString[14])
	logline.TurnAroundTime, _ = strconv.Atoi(reString[15])
	logline.Referrer = reString[16]
	logline.UserAgent = reString[17]
	ua := user_agent.New(reString[17])
	logline.UserAgentBrowser, logline.UserAgentBrowserVersion = ua.Browser()
	logline.UserAgentPlatform = ua.Platform()
	if ua.Mobile() {
		logline.UserAgentMobile = 1
	} else {
		logline.UserAgentMobile = 0
	}
	if ua.Bot() {
		logline.UserAgentBot = 1
	} else {
		logline.UserAgentBot = 0
	}

	logline.UserAgentOS = ua.OS()
	logline.UserAgentEngine, logline.UserAgentEngineVersion = ua.Engine()
	logline.VersionId = reString[18]

	return logline

}

func downloadFile(bucketFile string, b *s3.Bucket, DeleteInternalLogs string, DeleteLogsFromS3 string, DBLocation string, wg *sync.WaitGroup) (new_rows int, old_rows int) {
	defer wg.Done()
	downloadBytes, err := b.Get(bucketFile)
	if err != nil {
		log.Println(err)
	}
	if DeleteLogsFromS3 == "true" {
		err = b.Del(bucketFile)
		if err != nil {
			log.Println(err)
		}
	}

	return parseS3Log(string(downloadBytes), DeleteInternalLogs, DBLocation)
}

func parseS3Log(awsLogFile string, DeleteInternalLogs string, DBLocation string) (int, int) {

	db, _ := sql.Open("sqlite3", "file:"+DBLocation+"?cache=shared&mode=rwc")
	re := regexp.MustCompile(`(\S+) (\S+) \[(.*?)\] (\S+) (\S+) (\S+) (\S+) (\S+) "([^"]+)" (\S+) (\S+) (\S+) (\S+) (\S+) (\S+) "([^"]+)" "([^"]+)" (\S)`)

	matches := re.FindAllStringSubmatch(awsLogFile, -1)

	var rowsAdded int
	var errorCnt int

	for i := 0; i < len(matches); i++ {
		tmp := newLog(matches[i])
		if DeleteInternalLogs == "true" {
			if !internalUA[tmp.UserAgent] {
				_, err := db.Exec(InsertStmt, tmp.BucketOwner, tmp.Bucket, tmp.Time, tmp.RemoteIP, tmp.RemoteCity, tmp.RemoteCountry, tmp.RemoteLat, tmp.RemoteLng, tmp.Requester, tmp.RequestID, tmp.Operation, tmp.Key, tmp.RequestURI, tmp.HTTPstatus, tmp.ErrorCode, tmp.BytesSent, tmp.ObjectSize, tmp.TotalTime, tmp.TurnAroundTime, tmp.Referrer, tmp.UserAgent, tmp.UserAgentBrowser, tmp.UserAgentBrowserVersion, tmp.UserAgentPlatform, tmp.UserAgentMobile, tmp.UserAgentBot, tmp.UserAgentOS, tmp.UserAgentEngine, tmp.UserAgentEngineVersion, tmp.VersionId)
				if err != nil {
					fmt.Println(err)
					errorCnt += 1
				} else {
					rowsAdded += 1
				}
			}
		} else {
			fmt.Println(tmp.BucketOwner, tmp.Bucket, tmp.Time, tmp.RemoteIP, tmp.RemoteCity, tmp.RemoteCountry, tmp.RemoteLat, tmp.RemoteLng, tmp.Requester, tmp.RequestID, tmp.Operation, tmp.Key, tmp.RequestURI, tmp.HTTPstatus, tmp.ErrorCode, tmp.BytesSent, tmp.ObjectSize, tmp.TotalTime, tmp.TurnAroundTime, tmp.Referrer, tmp.UserAgent, tmp.UserAgentBrowser, tmp.UserAgentBrowserVersion, tmp.UserAgentPlatform, tmp.UserAgentMobile, tmp.UserAgentBot, tmp.UserAgentOS, tmp.UserAgentEngine, tmp.UserAgentEngineVersion, tmp.VersionId)
			_, err := db.Exec(InsertStmt, tmp.BucketOwner, tmp.Bucket, tmp.Time, tmp.RemoteIP, tmp.RemoteCity, tmp.RemoteCountry, tmp.RemoteLat, tmp.RemoteLng, tmp.Requester, tmp.RequestID, tmp.Operation, tmp.Key, tmp.RequestURI, tmp.HTTPstatus, tmp.ErrorCode, tmp.BytesSent, tmp.ObjectSize, tmp.TotalTime, tmp.TurnAroundTime, tmp.Referrer, tmp.UserAgent, tmp.UserAgentBrowser, tmp.UserAgentBrowserVersion, tmp.UserAgentPlatform, tmp.UserAgentMobile, tmp.UserAgentBot, tmp.UserAgentOS, tmp.UserAgentEngine, tmp.UserAgentEngineVersion, tmp.VersionId)
			if err != nil {
				fmt.Println(err)
				errorCnt += 1
			} else {
				rowsAdded += 1
			}

		}

	}
	return rowsAdded, errorCnt
}

type logResult struct {
	Date          string
	Time          string
	Remoteip      string
	Remotecity    string
	Remotecountry string
	Key           string
	Useragent     string
}

func createHTMLreport(DBLocation string, reportLocation string) {
	lastSnapshotDate := time.Now().Add(-24 * 10 * time.Hour)
	sqlStmt := `SELECT substr(time,6,5) as date, substr(time,11,6) as time, 
	remoteip, remotecity, remotecountry, key, useragent 
    FROM s3_logs 
    WHERE useragentbot=0 and useragentbrowser <> "Bot" and HTTPstatus="200" and time>=?
    ORDER BY date desc, time desc
	`
	db, err := sql.Open("sqlite3", "file:"+DBLocation+"?cache=shared&mode=rwc")
	if err != nil {
		fmt.Println(err)
		log.Fatal(err)
	}
	defer db.Close()
	db.Exec("update s3_logs set useragentbot=1 where useragent in (select useragentstring from bots)")

	rows, err := db.Query(sqlStmt, lastSnapshotDate.Format("2006-01-02 00:00:00.000000"))
	if err != nil {
		fmt.Println(err)
		log.Fatal(err)
	}
	defer rows.Close()

	tmpl, err := template.New("test").Parse(`
	<!doctype html>
	<html>
	<head>
	<meta charset="utf-8">
	<title>Logs from last 7 days</title>
	<style>
	#logs table{font-family:'Segoe UI';font-size:11px;text-align:left;color:#2E4758;width:100%;}
	#logs td{padding: 0 5px;max-width:600px;word-wrap: break-word;white-space:nowrap;overflow: hidden;text-overflow: ellipsis;}
	#logs table{border-collapse:collapse;}
	#logs table tr:nth-child(even) {background:#D0E0EB;}
	#logs th{border-bottom:1px solid #ddd;padding:0 5px 10px}
	</style>
	</head>
	<body id="logs">
	<table>
	<tr><th>date</th><th>time</th><th>ip</th><th>city</th><th>country</th><th>path</th><th>Useragent</th></tr>
	{{range .}}
	<tr><td>{{.Date}}</td><td>{{.Time}}</td><td>{{.Remoteip}}</td><td>{{.Remotecity}}</td><td>{{.Remotecountry}}</td><td>{{.Key}}</td><td>{{.Useragent}}</td></tr>{{end}}
	</table>
	</body>
	</html>
`)
	if err != nil {
		fmt.Println(err)
		log.Fatal(err)
	}

	var logsToOutput []logResult

	for rows.Next() {
		var singleLog logResult
		var date string
		var time string
		var remoteip string
		var remotecity string
		var remotecountry string
		var key string
		var useragent string
		rows.Scan(&date, &time, &remoteip, &remotecity, &remotecountry, &key, &useragent)
		singleLog.Date = date
		singleLog.Time = time
		singleLog.Remoteip = remoteip
		singleLog.Remotecity = remotecity
		singleLog.Remotecountry = remotecountry
		singleLog.Key = key
		singleLog.Useragent = useragent
		logsToOutput = append(logsToOutput, singleLog)
	}
	fo, err := os.Create(reportLocation)
	if err != nil {
		panic(err)
	}
	defer fo.Close()

	tmpl.Execute(fo, logsToOutput)

}

func geolocate(DBLocation string) {
	lastSnapshotDate := time.Now().Add(-24 * 20 * time.Hour)
	sqlStmt := "select distinct RemoteIp from s3_logs where RemoteCountry='' and time>=?"
	updateStmt := "update s3_logs set RemoteCity=?, RemoteCountry=?, RemoteLat=?, RemoteLng=? where RemoteIp=?;"
	db, err := sql.Open("sqlite3", "file:"+DBLocation+"?cache=shared&mode=rwc")

	if err != nil {
		fmt.Println(err)
		log.Fatal(err)
	}
	rows, err := db.Query(sqlStmt, lastSnapshotDate.Format("2006-01-02 00:00:00.000000"))
	if err != nil {
		fmt.Println(err)
		log.Fatal(err)
	}

	var ips []string
	for rows.Next() {
		var ip string
		rows.Scan(&ip)
		ips = append(ips, ip)
	}
	rows.Close()

	for i := 0; i < len(ips); i++ {
		var tmp IpApiResponse
		tmp = getGeolocation(ips[i])

		if tmp.City != "" {
			_, err := db.Exec(updateStmt, tmp.City, tmp.Country, tmp.Lat, tmp.Lon, tmp.Query)
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

func main() {
	stopwatch := time.Now()
	conf := readConfiguration()
	fmt.Print("-- Reading configuration:  ")
	fmt.Println(time.Since(stopwatch))

	auth := aws.Auth{SecretKey: conf.AwsSecretAccessKey, AccessKey: conf.AwsAccessKeyID}

	regionNameMap := map[string]aws.Region{
		"us-east-1":      aws.USEast,
		"us-west-1":      aws.USWest,
		"us-west-2":      aws.USWest2,
		"eu-west-1":      aws.EUWest,
		"eu-central-1":   aws.EUCentral,
		"ap-northeast-1": aws.APNortheast,
		//"ap-northeast-2": aws.APNortheast2,
		"ap-southeast-1": aws.APSoutheast,
		"ap-southeast-2": aws.APSoutheast2,
		"sa-east-1":      aws.SAEast,
	}

	connection := s3.New(auth, regionNameMap[conf.AwsRegion])
	bucket := connection.Bucket(conf.AwsBucketName)

	response, err := bucket.List("logs/access_log", "", "", 1000)

	if err != nil {
		log.Println(err)
	}

	var wg sync.WaitGroup
	for _, objects := range response.Contents {
		wg.Add(1)
		go downloadFile(objects.Key, bucket, conf.DeleteInternalLogs, conf.DeleteLogsFromS3, conf.DBLocation, &wg)
	}
	fmt.Print("-- Downloading files:  ")
	wg.Wait()

	fmt.Println(time.Since(stopwatch))
	if conf.AddGeolocation == "true" {
		fmt.Print("-- Geolocating:  ")
		geolocate(conf.DBLocation)
	}

	fmt.Println(time.Since(stopwatch))
	if conf.ReportLocation != "" {
		fmt.Print("-- Creating raport:  ")
		createHTMLreport(conf.DBLocation, conf.ReportLocation)
		fmt.Println(time.Since(stopwatch))
	}

}

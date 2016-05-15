# About
**goS3logs** is a tool to download access logs from Amazon S3 to local machine. Logs are parsed and inserted to sqlite database with additional information about geolocation (based on IP address) and user agent.
This tool is halpful when you want to see visits to static site hosted on Amazon S3. I use it to see request to [my website](http://grotos.net).

# Features
1. Download all access logs to s3 bucket
2. Add geolocation of requests
3. Parse user agents
4. Mark bots
5. Create report with recent visits

# Usage
To use this tool you have to create a configuration file named "conf.json" in the directory of binary.
The example file:

    {
    	"AWS_BUCKET_NAME":"grotos.net",
    	"AWS_ACCESS_KEY_ID":"?",
    	"AWS_SECRET_ACCESS_KEY":"?",
    	"AWS_REGION":"eu-west-1",
    	"AddGeolocation":"true",
    	"DeleteLogsFromS3":"true",
    	"DeleteInternalLogs":"true",
    	"DBLocation":"D:\\S3logs.sqlite",
    	"ReportLocation":"D:\\S3logs.html"
    }

Option description:

* `AWS_BUCKET_NAME`: name of a S3 bucket
* `AWS_ACCESS_KEY_ID`: AWS access key id
* `AWS_SECRET_ACCESS_KEY`: AWS secret access key
* `AWS_REGION`: AWS region from [list](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-regions-availability-zones.html#concepts-available-regions)
* `AddGeolocation`: when set to `true` there will be added information about geolocation based on service [IP-API](http://ip-api.com) (be careful about API quota)
* `DeleteLogsFromS3`: when set to `true`, log files in S3 bucket will be deleted
* `DeleteInternalLogs`: when set to `true`, logs about accessing S3 bucket will be deleted
* `DBLocation`: a path to sqlite database with logs
* `ReportLocation`: a location of HTML file which will be created after downloading logs

Running application:

1. Compile go source code
2. Create config file with proper AWS information. When in doubt set `DeleteLogsFromS3` and `DeleteInternalLogs` to `false`.
3. Copy `S3logs.sqlite` database
4. Run binary

Running application will add your logs to `S3logs.sqlite` and create report with requests in last 7 days.

Licence: MIT
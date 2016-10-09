package grafana

import (
	gocf "github.com/crewjam/go-cloudformation"
	spartaCF "github.com/mweagle/Sparta/aws/cloudformation"
	"strings"
)

// http://www.netinstructions.com/installing-influxdb-and-grafana-on-an-ec2-instance/
const grafanaUserData = `#!/bin/bash -xe
# http://simonjbeaumont.com/posts/docker-dashboard/
# http://www.netinstructions.com/installing-influxdb-and-grafana-on-an-ec2-instance/

INFLUX_DB_NAME=lambda
yum upgrade -y

curl https://dl.influxdata.com/influxdb/releases/influxdb-1.0.0.x86_64.rpm -o /home/ec2-user/influx.rpm
yum localinstall /home/ec2-user/influx.rpm -y
service influxdb start
rm /home/ec2-user/influx.rpm

# Create the influx db
until /usr/bin/influx -execute "SHOW databases"; do sleep 1; done
/usr/bin/influx -format=json -pretty -execute "CREATE DATABASE $INFLUX_DB_NAME"

yum install https://grafanarel.s3.amazonaws.com/builds/grafana-3.1.1-1470047149.x86_64.rpm -y
service grafana-server start
curl -u admin:admin -H "Content-Type: application/json" -X POST --data-binary '{"name":"lambda","type":"influxdb","typeLogoUrl":"public/app/plugins/datasource/influxdb/img/influxdb_logo.svg","access":"proxy","url":"http://localhost:8086","password":"na","user":"na","database":"lambda","basicAuth":false,"basicAuthUser":"","basicAuthPassword":"","withCredentials":false,"isDefault":true}' http://localhost:3000/api/datasources

# Create the default dashboard

cat <<GRAFANA_DASHBOARD_DEFINITION > /home/ec2-user/dashboard.json
{
	"overwrite": true,
	"inputs": [{}],
	"dashboard" : {
		"id": null,
		"title": "Sparta Hello World",
		"tags": [],
		"style": "dark",
		"timezone": "browser",
		"editable": true,
		"hideControls": false,
		"sharedCrosshair": false,
		"rows": [
			{
				"collapse": false,
				"editable": true,
				"height": "250px",
				"panels": [
					{
						"aliasColors": {},
						"bars": false,
						"datasource": "lambda",
						"editable": true,
						"error": false,
						"fill": 1,
						"grid": {
							"threshold1": null,
							"threshold1Color": "rgba(216, 200, 27, 0.27)",
							"threshold2": null,
							"threshold2Color": "rgba(234, 112, 112, 0.22)"
						},
						"id": 1,
						"isNew": true,
						"legend": {
							"avg": false,
							"current": false,
							"max": false,
							"min": false,
							"show": true,
							"total": false,
							"values": false
						},
						"lines": true,
						"linewidth": 2,
						"links": [],
						"nullPointMode": "connected",
						"percentage": false,
						"pointradius": 5,
						"points": false,
						"renderer": "flot",
						"seriesOverrides": [],
						"span": 12,
						"stack": false,
						"steppedLine": false,
						"targets": [
							{
								"dsType": "influxdb",
								"groupBy": [],
								"measurement": "HelloWorld.count",
								"policy": "default",
								"query": "SELECT sum(\"value\") FROM \"HelloWorld.count\" WHERE $timeFilter GROUP BY time()",
								"rawQuery": false,
								"refId": "A",
								"resultFormat": "time_series",
								"select": [
									[
										{
											"type": "field",
											"params": [
												"value"
											]
										}
									]
								],
								"tags": [],
								"hide": false
							}
						],
						"timeFrom": null,
						"timeShift": null,
						"title": "Lambda Call Count",
						"tooltip": {
							"msResolution": true,
							"shared": true,
							"sort": 0,
							"value_type": "cumulative"
						},
						"type": "graph",
						"xaxis": {
							"show": true
						},
						"yaxes": [
							{
								"format": "short",
								"label": null,
								"logBase": 1,
								"max": null,
								"min": null,
								"show": true
							},
							{
								"format": "short",
								"label": null,
								"logBase": 1,
								"max": null,
								"min": null,
								"show": true
							}
						]
					}
				],
				"title": "Row"
			}
		],
		"time": {
			"from": "now-15m",
			"to": "now"
		},
		"timepicker": {
			"refresh_intervals": [
				"5s",
				"10s",
				"30s",
				"1m",
				"5m",
				"15m",
				"30m",
				"1h",
				"2h",
				"1d"
			],
			"time_options": [
				"5m",
				"15m",
				"1h",
				"6h",
				"12h",
				"24h",
				"2d",
				"7d",
				"30d"
			]
		},
		"templating": {
			"list": []
		},
		"annotations": {
			"list": []
		},
  	"refresh": "5s",
		"schemaVersion": 12,
		"version": 1,
		"links": [],
		"gnetId": null
	}
}
GRAFANA_DASHBOARD_DEFINITION

curl -i -u admin:admin -H "Content-Type: application/json" -X POST --data @/home/ec2-user/dashboard.json http://localhost:3000/api/dashboards/db
`

func Stack(sshKeyName string, grafanaDNSName string) (*gocf.Template, error) {
	grafanaStack := gocf.NewTemplate()

	// The SG we'll use for the Import/Export name
	grafanaSG := gocf.EC2SecurityGroup{
		GroupDescription: gocf.String("Grafana SG"),
		SecurityGroupIngress: &gocf.EC2SecurityGroupRuleList{
			gocf.EC2SecurityGroupRule{
				CidrIp:     gocf.String("0.0.0.0/0"),
				FromPort:   gocf.Integer(22),
				ToPort:     gocf.Integer(22),
				IpProtocol: gocf.String("tcp"),
			},
			gocf.EC2SecurityGroupRule{
				CidrIp:     gocf.String("0.0.0.0/0"),
				FromPort:   gocf.Integer(3000),
				ToPort:     gocf.Integer(3000),
				IpProtocol: gocf.String("tcp"),
			},
			gocf.EC2SecurityGroupRule{
				CidrIp:     gocf.String("0.0.0.0/0"),
				FromPort:   gocf.Integer(8086),
				ToPort:     gocf.Integer(8086),
				IpProtocol: gocf.String("tcp"),
			},
		},
	}
	grafanaStack.AddResource("GrafanaSG", grafanaSG)

	// Create a single EC2 instance with userdata to install grafana
	userdataReader := strings.NewReader(grafanaUserData)
	userdata, userdataErr := spartaCF.ConvertToTemplateExpression(userdataReader, nil)
	if nil != userdataErr {
		return nil, userdataErr
	}
	grafanaEC2 := gocf.EC2Instance{
		KeyName:          gocf.String(sshKeyName),
		SecurityGroups:   gocf.StringList(gocf.Ref("GrafanaSG")),
		UserData:         gocf.Base64(userdata),
		InstanceType:     gocf.String("m3.xlarge"),
		ImageId:          gocf.String("ami-dd4894bd"),
		AvailabilityZone: gocf.String(""),
		Tags: []gocf.ResourceTag{
			gocf.ResourceTag{
				Key:   gocf.String("Name"),
				Value: gocf.String("Grafana"),
			},
		},
	}
	grafanaStack.AddResource("GrafanaEC2", grafanaEC2)

	// Add some outputs
	grafanaStack.Outputs[grafanaDNSName] = &gocf.Output{
		Description: "Grafana EC2 Public DNS Name",
		Value:       gocf.GetAtt("GrafanaEC2", "PublicDnsName"),
		Export: &gocf.OutputExport{
			Name: gocf.String(grafanaDNSName),
		},
	}
	grafanaStack.Outputs["DashboardURL"] = &gocf.Output{
		Description: "Grafana Dashboard",
		Value: gocf.Join("",
			gocf.String("http://"),
			gocf.GetAtt("GrafanaEC2", "PublicDnsName"),
			gocf.String(":3000")),
	}
	return grafanaStack, nil
}

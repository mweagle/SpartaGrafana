# SpartaGrafana

![Grafana Log](/resources/GrafanaMetrics.gif?raw=true "Grafana Metrics")

*Note*: Requires Sparta 0.9.2 or later

This is a multiple CloudFormation-stack [Sparta](http://gosparta.io)-based application that includes provisioning a [Grafana](http://grafana.org) host running on a single EC2 instance.

The Grafana service is provisioned via a [WorkflowHook](https://godoc.org/github.com/mweagle/Sparta#WorkflowHooks) that defines and provisions a separate CloudFormation stack. The *GrafanaStack* exports the EC2 hostname's PublicDNS name which is then referenced by the `HelloWorld` lambda' function's metadata. This binding ensures that the *GrafanaStack* cannot be deleted until the referring Sparta application (*SpartaGrafanaPublisher*) is deleted.

The *SpartaGrafanaPublisher* discovers the `PublicDNSName` during `init()` by querying CloudFormation:

```golang
  awsSession := session.New()
  cloudFormationSvc := cloudformation.New(awsSession)
  params := &cloudformation.DescribeStacksInput{
    StackName: aws.String(GrafanaStackName),
  }
  outputResults, outputResultsErr := cloudFormationSvc.DescribeStacks(params)
```

Once the DNSName is determined, the *SpartaGrafanaPublisher* service creates an [Influx DB Client](github.com/vrischmann/go-metrics-influxdb) to publish [go-metrics](github.com/rcrowley/go-metrics) to the Grafana instance.

To view the single automatically-generated Grafana dashboard, login to the Grafana host (see the *GrafanaStack* CloudFormation Outputs fror the domain) using `admin/admin` and navigate to the `Sparta Hello World` dashboard. By default, the `HelloWorld.count` counter is tracked, which is the number of lambda reported invocations.

To view the dashboard in action, navigate to the AWS Lambda Console and "Test" the function to begin publishing metrics.
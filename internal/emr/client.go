package emr

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awsemr "github.com/aws/aws-sdk-go-v2/service/emr"
	"github.com/aws/aws-sdk-go-v2/service/emr/types"
)

type Cluster struct {
	ID                    string
	Name                  string
	State                 string
	PrimaryNodePrivateDNS string
	CreatedAt             string
}

type ClusterDetail struct {
	Cluster
	ReleaseLabel      string
	LogURI            string
	ServiceRole       string
	StepConcurrency   string
	StateChangeReason string
	ReadyAt           string
	EndedAt           string
	Elapsed           string
	Applications      []string
	Instances         []InstanceSummary
}

type StepPage struct {
	Steps      []Step
	NextMarker string
}

type InstanceSummary struct {
	Kind  string
	Type  string
	Count string
}

type Step struct {
	ID        string
	Name      string
	State     string
	CreatedAt string
	StartedAt string
	EndedAt   string
	Elapsed   string
	LogURI    string
}

type YarnApplication struct {
	ID                       string
	Name                     string
	State                    string
	User                     string
	Queue                    string
	FinalStatus              string
	Progress                 string
	TrackingURL              string
	Diagnostics              string
	ApplicationType          string
	ApplicationTags          string
	Priority                 string
	StartedAt                string
	FinishedAt               string
	Elapsed                  string
	AMContainerLogs          string
	AMHostHTTPAddress        string
	AllocatedMB              string
	AllocatedVCores          string
	ReservedMB               string
	ReservedVCores           string
	RunningContainers        string
	MemorySeconds            string
	VCoreSeconds             string
	QueueUsagePercentage     string
	ClusterUsagePercentage   string
	PreemptedResourceMB      string
	PreemptedResourceVCores  string
	NonAMContainersPreempted string
	AMContainersPreempted    string
	LogAggregationStatus     string
	UnmanagedApplication     string
	AppNodeLabelExpression   string
	AMNodeLabelExpression    string
}

func NewClient(ctx context.Context, region string) (*awsemr.Client, error) {
	options := []func(*config.LoadOptions) error{}
	if region != "" {
		options = append(options, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return awsemr.NewFromConfig(cfg), nil
}

func ListRunningClusters(ctx context.Context, region string) ([]Cluster, error) {
	client, err := NewClient(ctx, region)
	if err != nil {
		return nil, err
	}

	paginator := awsemr.NewListClustersPaginator(client, &awsemr.ListClustersInput{
		ClusterStates: []types.ClusterState{
			types.ClusterStateStarting,
			types.ClusterStateBootstrapping,
			types.ClusterStateRunning,
			types.ClusterStateWaiting,
		},
	})

	var clusters []Cluster
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list emr clusters: %w", err)
		}

		for _, cluster := range page.Clusters {
			clusters = append(clusters, Cluster{
				ID:        stringValue(cluster.Id),
				Name:      stringValue(cluster.Name),
				State:     clusterState(cluster.Status),
				CreatedAt: clusterCreatedAt(cluster.Status),
			})
		}
	}

	return clusters, nil
}

func GetClusterDetail(ctx context.Context, region string, clusterID string) (ClusterDetail, error) {
	client, err := NewClient(ctx, region)
	if err != nil {
		return ClusterDetail{}, err
	}

	output, err := client.DescribeCluster(ctx, &awsemr.DescribeClusterInput{
		ClusterId: aws.String(clusterID),
	})
	if err != nil {
		return ClusterDetail{}, fmt.Errorf("describe emr cluster %s: %w", clusterID, err)
	}
	if output.Cluster == nil {
		return ClusterDetail{}, fmt.Errorf("describe emr cluster %s: empty cluster response", clusterID)
	}

	cluster := output.Cluster
	primaryNodePrivateDNS := lookupPrimaryNodePrivateDNS(ctx, client, clusterID)
	detail := ClusterDetail{
		Cluster: Cluster{
			ID:                    stringValue(cluster.Id),
			Name:                  stringValue(cluster.Name),
			State:                 clusterState(cluster.Status),
			PrimaryNodePrivateDNS: primaryNodePrivateDNS,
			CreatedAt:             clusterCreatedAt(cluster.Status),
		},
		ReleaseLabel:      stringValue(cluster.ReleaseLabel),
		LogURI:            stringValue(cluster.LogUri),
		ServiceRole:       stringValue(cluster.ServiceRole),
		StepConcurrency:   int32Value(cluster.StepConcurrencyLevel),
		StateChangeReason: clusterStateChangeReason(cluster.Status),
		ReadyAt:           clusterTimelineTime(cluster.Status, "ready"),
		EndedAt:           clusterTimelineTime(cluster.Status, "ended"),
		Elapsed:           clusterElapsed(cluster.Status),
		Applications:      applications(cluster.Applications),
	}

	instances, err := listInstanceSummaries(ctx, client, clusterID)
	if err != nil {
		return ClusterDetail{}, err
	}
	detail.Instances = instances

	return detail, nil
}

func lookupPrimaryNodePrivateDNS(ctx context.Context, client *awsemr.Client, clusterID string) string {
	privateDNS, ok := lookupPrimaryNodePrivateDNSByInstanceGroup(ctx, client, clusterID)
	if ok {
		return privateDNS
	}

	return lookupPrimaryNodePrivateDNSByInstanceFleet(ctx, client, clusterID)
}

func lookupPrimaryNodePrivateDNSByInstanceGroup(ctx context.Context, client *awsemr.Client, clusterID string) (string, bool) {
	paginator := awsemr.NewListInstancesPaginator(client, &awsemr.ListInstancesInput{
		ClusterId: aws.String(clusterID),
		InstanceGroupTypes: []types.InstanceGroupType{
			types.InstanceGroupTypeMaster,
		},
		InstanceStates: []types.InstanceState{
			types.InstanceStateBootstrapping,
			types.InstanceStateRunning,
		},
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "-", false
		}

		for _, instance := range page.Instances {
			return stringValue(instance.PrivateDnsName), true
		}
	}

	return "-", false
}

func lookupPrimaryNodePrivateDNSByInstanceFleet(ctx context.Context, client *awsemr.Client, clusterID string) string {
	paginator := awsemr.NewListInstancesPaginator(client, &awsemr.ListInstancesInput{
		ClusterId:         aws.String(clusterID),
		InstanceFleetType: types.InstanceFleetTypeMaster,
		InstanceStates: []types.InstanceState{
			types.InstanceStateBootstrapping,
			types.InstanceStateRunning,
		},
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "-"
		}

		for _, instance := range page.Instances {
			return stringValue(instance.PrivateDnsName)
		}
	}

	return "-"
}

func ListStepsPage(ctx context.Context, region string, clusterID string, marker string) (StepPage, error) {
	client, err := NewClient(ctx, region)
	if err != nil {
		return StepPage{}, err
	}

	input := &awsemr.ListStepsInput{
		ClusterId: aws.String(clusterID),
	}
	if marker != "" {
		input.Marker = aws.String(marker)
	}

	page, err := client.ListSteps(ctx, input)
	if err != nil {
		return StepPage{}, fmt.Errorf("list emr cluster %s steps: %w", clusterID, err)
	}

	nextMarker := ""
	if page.Marker != nil {
		nextMarker = *page.Marker
	}
	var steps []Step
	for _, step := range page.Steps {
		steps = append(steps, Step{
			ID:        stringValue(step.Id),
			Name:      stringValue(step.Name),
			State:     stepState(step.Status),
			CreatedAt: stepTime(step.Status, "created"),
			StartedAt: stepTime(step.Status, "started"),
			EndedAt:   stepTime(step.Status, "ended"),
			Elapsed:   stepElapsed(step.Status),
			LogURI:    stringValue(step.LogUri),
		})
	}

	return StepPage{Steps: steps, NextMarker: nextMarker}, nil
}

func ListYarnApplications(ctx context.Context, host string) ([]YarnApplication, error) {
	if host == "" || host == "-" {
		return nil, nil
	}

	url := fmt.Sprintf("https://%s:8088/ws/v1/cluster/apps?states=RUNNING,FINISHED,FAILED,KILLED,ACCEPTED,NEW", host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new yarn request: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call yarn api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("yarn api %s returned status %s", url, resp.Status)
	}

	var payload struct {
		Apps struct {
			App []struct {
				ID                         string  `json:"id"`
				Name                       string  `json:"name"`
				State                      string  `json:"state"`
				User                       string  `json:"user"`
				Queue                      string  `json:"queue"`
				FinalStatus                string  `json:"finalStatus"`
				Progress                   float64 `json:"progress"`
				TrackingURL                string  `json:"trackingUrl"`
				Diagnostics                string  `json:"diagnostics"`
				ApplicationType            string  `json:"applicationType"`
				ApplicationTags            string  `json:"applicationTags"`
				Priority                   int64   `json:"priority"`
				StartedTime                int64   `json:"startedTime"`
				FinishedTime               int64   `json:"finishedTime"`
				ElapsedTime                int64   `json:"elapsedTime"`
				AMContainerLogs            string  `json:"amContainerLogs"`
				AMHostHTTPAddress          string  `json:"amHostHttpAddress"`
				AllocatedMB                int64   `json:"allocatedMB"`
				AllocatedVCores            int64   `json:"allocatedVCores"`
				ReservedMB                 int64   `json:"reservedMB"`
				ReservedVCores             int64   `json:"reservedVCores"`
				RunningContainers          int64   `json:"runningContainers"`
				MemorySeconds              int64   `json:"memorySeconds"`
				VCoreSeconds               int64   `json:"vcoreSeconds"`
				QueueUsagePercentage       float64 `json:"queueUsagePercentage"`
				ClusterUsagePercentage     float64 `json:"clusterUsagePercentage"`
				PreemptedResourceMB        int64   `json:"preemptedResourceMB"`
				PreemptedResourceVCores    int64   `json:"preemptedResourceVCores"`
				NumNonAMContainerPreempted int64   `json:"numNonAMContainerPreempted"`
				NumAMContainerPreempted    int64   `json:"numAMContainerPreempted"`
				LogAggregationStatus       string  `json:"logAggregationStatus"`
				UnmanagedApplication       bool    `json:"unmanagedApplication"`
				AppNodeLabelExpression     string  `json:"appNodeLabelExpression"`
				AMNodeLabelExpression      string  `json:"amNodeLabelExpression"`
			} `json:"app"`
		} `json:"apps"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode yarn api response: %w", err)
	}

	apps := make([]YarnApplication, 0, len(payload.Apps.App))
	for _, app := range payload.Apps.App {
		appID := app.ID
		if appID == "" {
			appID = "-"
		}
		name := app.Name
		if name == "" {
			name = "-"
		}
		user := app.User
		if user == "" {
			user = "-"
		}
		startedAt := "-"
		if app.StartedTime > 0 {
			startedAt = time.UnixMilli(app.StartedTime).Local().Format(time.DateTime)
		}
		finishedAt := "-"
		if app.FinishedTime > 0 {
			finishedAt = time.UnixMilli(app.FinishedTime).Local().Format(time.DateTime)
		}
		elapsed := formatYarnDuration(app.ElapsedTime)
		apps = append(apps, YarnApplication{
			ID:                       appID,
			Name:                     name,
			State:                    valueOrDash(app.State),
			User:                     user,
			Queue:                    valueOrDash(app.Queue),
			FinalStatus:              valueOrDash(app.FinalStatus),
			Progress:                 fmt.Sprintf("%.1f%%", app.Progress),
			TrackingURL:              valueOrDash(app.TrackingURL),
			Diagnostics:              valueOrDash(strings.Join(strings.Fields(app.Diagnostics), " ")),
			ApplicationType:          valueOrDash(app.ApplicationType),
			ApplicationTags:          valueOrDash(app.ApplicationTags),
			Priority:                 fmt.Sprintf("%d", app.Priority),
			StartedAt:                startedAt,
			FinishedAt:               finishedAt,
			Elapsed:                  elapsed,
			AMContainerLogs:          valueOrDash(app.AMContainerLogs),
			AMHostHTTPAddress:        valueOrDash(app.AMHostHTTPAddress),
			AllocatedMB:              fmt.Sprintf("%d", app.AllocatedMB),
			AllocatedVCores:          fmt.Sprintf("%d", app.AllocatedVCores),
			ReservedMB:               fmt.Sprintf("%d", app.ReservedMB),
			ReservedVCores:           fmt.Sprintf("%d", app.ReservedVCores),
			RunningContainers:        fmt.Sprintf("%d", app.RunningContainers),
			MemorySeconds:            fmt.Sprintf("%d", app.MemorySeconds),
			VCoreSeconds:             fmt.Sprintf("%d", app.VCoreSeconds),
			QueueUsagePercentage:     fmt.Sprintf("%.2f%%", app.QueueUsagePercentage),
			ClusterUsagePercentage:   fmt.Sprintf("%.2f%%", app.ClusterUsagePercentage),
			PreemptedResourceMB:      fmt.Sprintf("%d", app.PreemptedResourceMB),
			PreemptedResourceVCores:  fmt.Sprintf("%d", app.PreemptedResourceVCores),
			NonAMContainersPreempted: fmt.Sprintf("%d", app.NumNonAMContainerPreempted),
			AMContainersPreempted:    fmt.Sprintf("%d", app.NumAMContainerPreempted),
			LogAggregationStatus:     valueOrDash(app.LogAggregationStatus),
			UnmanagedApplication:     fmt.Sprintf("%t", app.UnmanagedApplication),
			AppNodeLabelExpression:   valueOrDash(app.AppNodeLabelExpression),
			AMNodeLabelExpression:    valueOrDash(app.AMNodeLabelExpression),
		})
	}

	for i := 0; i < len(apps); i++ {
		for j := i + 1; j < len(apps); j++ {
			if yarnAppIDCompare(apps[j].ID, apps[i].ID) > 0 {
				apps[i], apps[j] = apps[j], apps[i]
			}
		}
	}
	return apps, nil
}

func yarnAppIDCompare(a, b string) int {
	if a == "-" && b != "-" {
		return -1
	}
	if a != "-" && b == "-" {
		return 1
	}
	if a == b {
		return 0
	}
	if a > b {
		return 1
	}
	return -1
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func formatYarnDuration(ms int64) string {
	if ms <= 0 {
		return "0秒"
	}
	totalSeconds := ms / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	parts := make([]string, 0, 3)
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d时", hours))
	}
	if minutes > 0 || len(parts) > 0 {
		parts = append(parts, fmt.Sprintf("%d分", minutes))
	}
	parts = append(parts, fmt.Sprintf("%d秒", seconds))
	return strings.Join(parts, "")
}

func listInstanceSummaries(ctx context.Context, client *awsemr.Client, clusterID string) ([]InstanceSummary, error) {
	groups, groupErr := listInstanceGroupSummaries(ctx, client, clusterID)
	if groupErr == nil && len(groups) > 0 {
		return groups, nil
	}

	fleets, fleetErr := listInstanceFleetSummaries(ctx, client, clusterID)
	if fleetErr != nil {
		if groupErr != nil {
			return nil, fmt.Errorf("%w; %w", groupErr, fleetErr)
		}
		return nil, fleetErr
	}

	return fleets, nil
}

func listInstanceGroupSummaries(ctx context.Context, client *awsemr.Client, clusterID string) ([]InstanceSummary, error) {
	paginator := awsemr.NewListInstanceGroupsPaginator(client, &awsemr.ListInstanceGroupsInput{
		ClusterId: aws.String(clusterID),
	})

	var summaries []InstanceSummary
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list emr cluster %s instance groups: %w", clusterID, err)
		}

		for _, group := range page.InstanceGroups {
			summaries = append(summaries, InstanceSummary{
				Kind:  string(group.InstanceGroupType),
				Type:  stringValue(group.InstanceType),
				Count: int32Value(group.RunningInstanceCount),
			})
		}
	}

	return summaries, nil
}

func listInstanceFleetSummaries(ctx context.Context, client *awsemr.Client, clusterID string) ([]InstanceSummary, error) {
	paginator := awsemr.NewListInstanceFleetsPaginator(client, &awsemr.ListInstanceFleetsInput{
		ClusterId: aws.String(clusterID),
	})

	var summaries []InstanceSummary
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list emr cluster %s instance fleets: %w", clusterID, err)
		}

		for _, fleet := range page.InstanceFleets {
			summaries = append(summaries, InstanceSummary{
				Kind:  string(fleet.InstanceFleetType),
				Type:  instanceFleetTypes(fleet.InstanceTypeSpecifications),
				Count: int32Sum(fleet.ProvisionedOnDemandCapacity, fleet.ProvisionedSpotCapacity),
			})
		}
	}

	return summaries, nil
}

func applications(values []types.Application) []string {
	result := make([]string, 0, len(values))
	for _, app := range values {
		name := stringValue(app.Name)
		version := stringValue(app.Version)
		if version != "-" {
			name += " " + version
		}
		result = append(result, name)
	}
	if len(result) == 0 {
		return []string{"-"}
	}

	return result
}

func instanceFleetTypes(values []types.InstanceTypeSpecification) string {
	result := make([]string, 0, len(values))
	for _, spec := range values {
		result = append(result, stringValue(spec.InstanceType))
	}
	if len(result) == 0 {
		return "-"
	}

	return strings.Join(result, ",")
}

func clusterState(status *types.ClusterStatus) string {
	if status == nil || status.State == "" {
		return "-"
	}

	return string(status.State)
}

func clusterCreatedAt(status *types.ClusterStatus) string {
	if status == nil || status.Timeline == nil || status.Timeline.CreationDateTime == nil {
		return "-"
	}

	return status.Timeline.CreationDateTime.Local().Format(time.DateTime)
}

func clusterTimelineTime(status *types.ClusterStatus, field string) string {
	if status == nil || status.Timeline == nil {
		return "-"
	}

	var value *time.Time
	switch field {
	case "ready":
		value = status.Timeline.ReadyDateTime
	case "ended":
		value = status.Timeline.EndDateTime
	}
	if value == nil {
		return "-"
	}
	return value.Local().Format(time.DateTime)
}

func clusterElapsed(status *types.ClusterStatus) string {
	if status == nil || status.Timeline == nil || status.Timeline.CreationDateTime == nil {
		return "-"
	}

	end := time.Now()
	if status.Timeline.EndDateTime != nil {
		end = *status.Timeline.EndDateTime
	}
	if end.Before(*status.Timeline.CreationDateTime) {
		return "-"
	}
	return formatYarnDuration(end.Sub(*status.Timeline.CreationDateTime).Milliseconds())
}

func clusterStateChangeReason(status *types.ClusterStatus) string {
	if status == nil || status.StateChangeReason == nil {
		return "-"
	}

	code := string(status.StateChangeReason.Code)
	message := stringValue(status.StateChangeReason.Message)
	if code == "" {
		return message
	}
	if message == "-" {
		return code
	}
	return code + ": " + message
}

func stepState(status *types.StepStatus) string {
	if status == nil || status.State == "" {
		return "-"
	}

	return string(status.State)
}

func stepTime(status *types.StepStatus, field string) string {
	if status == nil || status.Timeline == nil {
		return "-"
	}

	var value *time.Time
	switch field {
	case "created":
		value = status.Timeline.CreationDateTime
	case "started":
		value = status.Timeline.StartDateTime
	case "ended":
		value = status.Timeline.EndDateTime
	}

	if value == nil {
		return "-"
	}

	return value.Local().Format(time.DateTime)
}

func stepElapsed(status *types.StepStatus) string {
	if status == nil || status.Timeline == nil || status.Timeline.StartDateTime == nil {
		return "-"
	}

	end := time.Now()
	if status.Timeline.EndDateTime != nil {
		end = *status.Timeline.EndDateTime
	}
	if end.Before(*status.Timeline.StartDateTime) {
		return "-"
	}

	return formatYarnDuration(end.Sub(*status.Timeline.StartDateTime).Milliseconds())
}

func stringValue(value *string) string {
	if value == nil || *value == "" {
		return "-"
	}

	return *value
}

func int32Value(value *int32) string {
	if value == nil {
		return "-"
	}

	return fmt.Sprintf("%d", *value)
}

func int32Sum(values ...*int32) string {
	var total int32
	for _, value := range values {
		if value != nil {
			total += *value
		}
	}

	return fmt.Sprintf("%d", total)
}

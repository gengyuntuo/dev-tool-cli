package emr

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	awsemr "github.com/aws/aws-sdk-go-v2/service/emr"
	"github.com/aws/aws-sdk-go-v2/service/emr/types"
)

type Cluster struct {
	ID        string
	Name      string
	State     string
	CreatedAt string
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

func stringValue(value *string) string {
	if value == nil || *value == "" {
		return "-"
	}

	return *value
}

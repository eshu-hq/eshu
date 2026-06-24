// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbedrockagent "github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	awsbedrockagenttypes "github.com/aws/aws-sdk-go-v2/service/bedrockagent/types"

	bedrocksvc "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/bedrock"
)

// draftAgentVersion is the working agent version the adapter reads sub-resources
// (action groups, knowledge bases) against. Bedrock keeps the editable
// configuration on the DRAFT version.
const draftAgentVersion = "DRAFT"

// ListAgents returns agent metadata. The foundation model id and associated
// knowledge base ids come from GetAgent and ListAgentKnowledgeBases. The agent
// Instruction (system prompt) and PromptOverrideConfiguration that GetAgent also
// returns are deliberately never copied: the scanner-owned Agent type has no
// field for either.
func (c *Client) ListAgents(ctx context.Context) ([]bedrocksvc.Agent, error) {
	paginator := awsbedrockagent.NewListAgentsPaginator(c.agent, &awsbedrockagent.ListAgentsInput{})
	var agents []bedrocksvc.Agent
	for paginator.HasMorePages() {
		var page *awsbedrockagent.ListAgentsOutput
		if err := c.page(ctx, "ListAgents", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.AgentSummaries {
			agent := bedrocksvc.Agent{
				ID:          aws.ToString(summary.AgentId),
				Name:        aws.ToString(summary.AgentName),
				Status:      string(summary.AgentStatus),
				Description: aws.ToString(summary.Description),
			}
			if err := c.enrichAgent(ctx, &agent); err != nil {
				return nil, err
			}
			kbIDs, err := c.agentKnowledgeBaseIDs(ctx, agent.ID)
			if err != nil {
				return nil, err
			}
			agent.KnowledgeBaseIDs = kbIDs
			tags, err := c.agentTags(ctx, agent.ARN)
			if err != nil {
				return nil, err
			}
			agent.Tags = tags
			agents = append(agents, agent)
		}
	}
	return agents, nil
}

// enrichAgent reads the agent ARN and foundation model id from GetAgent. It
// deliberately ignores Agent.Instruction and Agent.PromptOverrideConfiguration;
// those are valuable IP the scanner-owned Agent type has no field to hold.
func (c *Client) enrichAgent(ctx context.Context, agent *bedrocksvc.Agent) error {
	id := strings.TrimSpace(agent.ID)
	if id == "" {
		return nil
	}
	var output *awsbedrockagent.GetAgentOutput
	if err := c.page(ctx, "GetAgent", func(callCtx context.Context) (err error) {
		output, err = c.agent.GetAgent(callCtx, &awsbedrockagent.GetAgentInput{
			AgentId: aws.String(id),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil || output.Agent == nil {
		return nil
	}
	agent.ARN = aws.ToString(output.Agent.AgentArn)
	agent.FoundationModel = aws.ToString(output.Agent.FoundationModel)
	agent.CreationTime = aws.ToTime(output.Agent.CreatedAt)
	if status := string(output.Agent.AgentStatus); status != "" {
		agent.Status = status
	}
	return nil
}

// agentKnowledgeBaseIDs lists the knowledge bases associated with one agent's
// DRAFT version.
func (c *Client) agentKnowledgeBaseIDs(ctx context.Context, agentID string) ([]string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, nil
	}
	paginator := awsbedrockagent.NewListAgentKnowledgeBasesPaginator(c.agent, &awsbedrockagent.ListAgentKnowledgeBasesInput{
		AgentId:      aws.String(agentID),
		AgentVersion: aws.String(draftAgentVersion),
	})
	var ids []string
	for paginator.HasMorePages() {
		var page *awsbedrockagent.ListAgentKnowledgeBasesOutput
		if err := c.page(ctx, "ListAgentKnowledgeBases", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.AgentKnowledgeBaseSummaries {
			if kbID := strings.TrimSpace(aws.ToString(summary.KnowledgeBaseId)); kbID != "" {
				ids = append(ids, kbID)
			}
		}
	}
	return ids, nil
}

// ListAgentActionGroups returns action group metadata for every agent's DRAFT
// version. The Lambda executor ARN comes from GetAgentActionGroup. The
// action-group API schema body and function schema that GetAgentActionGroup
// also returns are deliberately never copied: the scanner-owned
// AgentActionGroup type has no field for either.
func (c *Client) ListAgentActionGroups(ctx context.Context) ([]bedrocksvc.AgentActionGroup, error) {
	agentIDs, err := c.listAgentIDs(ctx)
	if err != nil {
		return nil, err
	}
	var groups []bedrocksvc.AgentActionGroup
	for _, agentID := range agentIDs {
		paginator := awsbedrockagent.NewListAgentActionGroupsPaginator(c.agent, &awsbedrockagent.ListAgentActionGroupsInput{
			AgentId:      aws.String(agentID),
			AgentVersion: aws.String(draftAgentVersion),
		})
		for paginator.HasMorePages() {
			var page *awsbedrockagent.ListAgentActionGroupsOutput
			if err := c.page(ctx, "ListAgentActionGroups", func(callCtx context.Context) (err error) {
				page, err = paginator.NextPage(callCtx)
				return err
			}); err != nil {
				return nil, err
			}
			for _, summary := range page.ActionGroupSummaries {
				group := bedrocksvc.AgentActionGroup{
					AgentID: agentID,
					ID:      aws.ToString(summary.ActionGroupId),
					Name:    aws.ToString(summary.ActionGroupName),
					State:   string(summary.ActionGroupState),
				}
				if err := c.enrichActionGroup(ctx, &group); err != nil {
					return nil, err
				}
				groups = append(groups, group)
			}
		}
	}
	return groups, nil
}

// enrichActionGroup reads the Lambda executor ARN from GetAgentActionGroup. It
// deliberately ignores ApiSchema and FunctionSchema; those are often customer
// IP the scanner-owned AgentActionGroup type has no field to hold.
func (c *Client) enrichActionGroup(ctx context.Context, group *bedrocksvc.AgentActionGroup) error {
	agentID := strings.TrimSpace(group.AgentID)
	groupID := strings.TrimSpace(group.ID)
	if agentID == "" || groupID == "" {
		return nil
	}
	var output *awsbedrockagent.GetAgentActionGroupOutput
	if err := c.page(ctx, "GetAgentActionGroup", func(callCtx context.Context) (err error) {
		output, err = c.agent.GetAgentActionGroup(callCtx, &awsbedrockagent.GetAgentActionGroupInput{
			AgentId:       aws.String(agentID),
			AgentVersion:  aws.String(draftAgentVersion),
			ActionGroupId: aws.String(groupID),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil || output.AgentActionGroup == nil {
		return nil
	}
	group.LambdaARN = lambdaExecutorARN(output.AgentActionGroup.ActionGroupExecutor)
	return nil
}

// lambdaExecutorARN extracts the Lambda function ARN from an action-group
// executor union, returning "" for the built-in control return executor.
func lambdaExecutorARN(executor awsbedrockagenttypes.ActionGroupExecutor) string {
	if lambda, ok := executor.(*awsbedrockagenttypes.ActionGroupExecutorMemberLambda); ok {
		return strings.TrimSpace(lambda.Value)
	}
	return ""
}

// ListKnowledgeBases returns knowledge base metadata. The embedding model ARN
// comes from GetKnowledgeBase and the data source endpoint refs come from
// ListDataSources plus GetDataSource. No ingested document content is read: the
// adapter never calls GetKnowledgeBaseDocuments or ListKnowledgeBaseDocuments,
// and the scanner-owned KnowledgeBase type has no field for document content.
func (c *Client) ListKnowledgeBases(ctx context.Context) ([]bedrocksvc.KnowledgeBase, error) {
	paginator := awsbedrockagent.NewListKnowledgeBasesPaginator(c.agent, &awsbedrockagent.ListKnowledgeBasesInput{})
	var bases []bedrocksvc.KnowledgeBase
	for paginator.HasMorePages() {
		var page *awsbedrockagent.ListKnowledgeBasesOutput
		if err := c.page(ctx, "ListKnowledgeBases", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.KnowledgeBaseSummaries {
			kb := bedrocksvc.KnowledgeBase{
				ID:          aws.ToString(summary.KnowledgeBaseId),
				Name:        aws.ToString(summary.Name),
				Status:      string(summary.Status),
				Description: aws.ToString(summary.Description),
			}
			if err := c.enrichKnowledgeBase(ctx, &kb); err != nil {
				return nil, err
			}
			sources, err := c.knowledgeBaseDataSources(ctx, kb.ID)
			if err != nil {
				return nil, err
			}
			kb.DataSources = sources
			tags, err := c.agentTags(ctx, kb.ARN)
			if err != nil {
				return nil, err
			}
			kb.Tags = tags
			bases = append(bases, kb)
		}
	}
	return bases, nil
}

// enrichKnowledgeBase reads the knowledge base ARN and embedding model ARN from
// GetKnowledgeBase. It reads the embedding model reference only; ingested
// document content is never returned by GetKnowledgeBase and is never read.
func (c *Client) enrichKnowledgeBase(ctx context.Context, kb *bedrocksvc.KnowledgeBase) error {
	id := strings.TrimSpace(kb.ID)
	if id == "" {
		return nil
	}
	var output *awsbedrockagent.GetKnowledgeBaseOutput
	if err := c.page(ctx, "GetKnowledgeBase", func(callCtx context.Context) (err error) {
		output, err = c.agent.GetKnowledgeBase(callCtx, &awsbedrockagent.GetKnowledgeBaseInput{
			KnowledgeBaseId: aws.String(id),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil || output.KnowledgeBase == nil {
		return nil
	}
	kb.ARN = aws.ToString(output.KnowledgeBase.KnowledgeBaseArn)
	kb.CreationTime = aws.ToTime(output.KnowledgeBase.CreatedAt)
	kb.EmbeddingModelARN = embeddingModelARN(output.KnowledgeBase.KnowledgeBaseConfiguration)
	return nil
}

// embeddingModelARN extracts the vector embedding model ARN from a knowledge
// base configuration, returning "" for non-vector configurations.
func embeddingModelARN(config *awsbedrockagenttypes.KnowledgeBaseConfiguration) string {
	if config == nil || config.VectorKnowledgeBaseConfiguration == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(config.VectorKnowledgeBaseConfiguration.EmbeddingModelArn))
}

// knowledgeBaseDataSources lists the data sources for one knowledge base and
// reads each source's endpoint reference (S3 bucket ARN, Confluence/SharePoint/
// web URL) from GetDataSource. It never reads ingested document content.
func (c *Client) knowledgeBaseDataSources(ctx context.Context, knowledgeBaseID string) ([]bedrocksvc.KnowledgeBaseDataSource, error) {
	knowledgeBaseID = strings.TrimSpace(knowledgeBaseID)
	if knowledgeBaseID == "" {
		return nil, nil
	}
	paginator := awsbedrockagent.NewListDataSourcesPaginator(c.agent, &awsbedrockagent.ListDataSourcesInput{
		KnowledgeBaseId: aws.String(knowledgeBaseID),
	})
	var sources []bedrocksvc.KnowledgeBaseDataSource
	for paginator.HasMorePages() {
		var page *awsbedrockagent.ListDataSourcesOutput
		if err := c.page(ctx, "ListDataSources", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.DataSourceSummaries {
			source := bedrocksvc.KnowledgeBaseDataSource{
				ID:   aws.ToString(summary.DataSourceId),
				Name: aws.ToString(summary.Name),
			}
			if err := c.enrichDataSource(ctx, knowledgeBaseID, &source); err != nil {
				return nil, err
			}
			sources = append(sources, source)
		}
	}
	return sources, nil
}

// enrichDataSource reads the connector type and endpoint reference from
// GetDataSource. It reads only the addressing fields (S3 bucket ARN, host/site/
// seed URL); it never reads ingested content.
func (c *Client) enrichDataSource(ctx context.Context, knowledgeBaseID string, source *bedrocksvc.KnowledgeBaseDataSource) error {
	dataSourceID := strings.TrimSpace(source.ID)
	if dataSourceID == "" {
		return nil
	}
	var output *awsbedrockagent.GetDataSourceOutput
	if err := c.page(ctx, "GetDataSource", func(callCtx context.Context) (err error) {
		output, err = c.agent.GetDataSource(callCtx, &awsbedrockagent.GetDataSourceInput{
			KnowledgeBaseId: aws.String(knowledgeBaseID),
			DataSourceId:    aws.String(dataSourceID),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil || output.DataSource == nil || output.DataSource.DataSourceConfiguration == nil {
		return nil
	}
	applyDataSourceConfiguration(source, output.DataSource.DataSourceConfiguration)
	return nil
}

// applyDataSourceConfiguration copies the connector type and endpoint reference
// from a data source configuration into the scanner-owned source. Inclusion
// prefixes, crawler scope rules, and credential secret references are not
// copied; only the addressing target is kept.
func applyDataSourceConfiguration(source *bedrocksvc.KnowledgeBaseDataSource, config *awsbedrockagenttypes.DataSourceConfiguration) {
	source.Type = string(config.Type)
	switch {
	case config.S3Configuration != nil:
		source.S3BucketARN = strings.TrimSpace(aws.ToString(config.S3Configuration.BucketArn))
	case config.ConfluenceConfiguration != nil && config.ConfluenceConfiguration.SourceConfiguration != nil:
		source.URL = strings.TrimSpace(aws.ToString(config.ConfluenceConfiguration.SourceConfiguration.HostUrl))
	case config.SharePointConfiguration != nil && config.SharePointConfiguration.SourceConfiguration != nil:
		source.URL = firstSharePointSiteURL(config.SharePointConfiguration.SourceConfiguration.SiteUrls)
	case config.WebConfiguration != nil:
		source.URL = firstWebSeedURL(config.WebConfiguration.SourceConfiguration)
	}
}

// firstSharePointSiteURL returns the first non-empty SharePoint site URL.
func firstSharePointSiteURL(siteURLs []string) string {
	for _, url := range siteURLs {
		if trimmed := strings.TrimSpace(url); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// firstWebSeedURL returns the first non-empty web-crawler seed URL.
func firstWebSeedURL(config *awsbedrockagenttypes.WebSourceConfiguration) string {
	if config == nil || config.UrlConfiguration == nil {
		return ""
	}
	for _, seed := range config.UrlConfiguration.SeedUrls {
		if url := strings.TrimSpace(aws.ToString(seed.Url)); url != "" {
			return url
		}
	}
	return ""
}

// listAgentIDs returns the agent ids for the boundary so action-group reads can
// fan out per agent.
func (c *Client) listAgentIDs(ctx context.Context) ([]string, error) {
	paginator := awsbedrockagent.NewListAgentsPaginator(c.agent, &awsbedrockagent.ListAgentsInput{})
	var ids []string
	for paginator.HasMorePages() {
		var page *awsbedrockagent.ListAgentsOutput
		if err := c.page(ctx, "ListAgents", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.AgentSummaries {
			if id := strings.TrimSpace(aws.ToString(summary.AgentId)); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

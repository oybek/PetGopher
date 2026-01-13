package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/go-co-op/gocron/v2"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// Config holds app configuration
type Config struct {
	GitlabBaseURL   string
	GitlabToken     string
	GitlabProjectID string
	SlackWebhookURL string
}

func loadConfig() *Config {
	return &Config{
		GitlabBaseURL:   os.Getenv("GITLAB_BASE_URL"),
		GitlabToken:     os.Getenv("GITLAB_TOKEN"),
		GitlabProjectID: os.Getenv("GITLAB_PROJECT_ID"),
		SlackWebhookURL: os.Getenv("SLACK_WEBHOOK_URL"),
	}
}

func main() {
	cfg := loadConfig()

	gitlabClient, err := newGitlabClient(cfg.GitlabBaseURL, cfg.GitlabToken)
	if err != nil {
		log.Fatalf("failed to create gitlab client: %v\n", err)
	}

	s, err := gocron.NewScheduler(gocron.WithLocation(time.FixedZone("UTC+6", 6*3600)))
	if err != nil {
		log.Fatalf("failed to create scheduler: %v\n", err)
	}

	_, err = s.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(gocron.NewAtTime(11, 30, 0))),
		gocron.NewTask(func() {
			log.Printf("running listOpenMRs at %s\n", time.Now().Format(time.RFC3339))

			mrs, err := listOpenMRs(gitlabClient, cfg.GitlabProjectID)
			if err != nil {
				log.Printf("failed to list open MRs: %v\n", err)
				return
			}

			msg := formatMRsForSlack(mrs)
			err = postToSlackChannel(cfg.SlackWebhookURL, msg)
			if err != nil {
				log.Printf("failed to post to slack: %v\n", err)
			}
		}),
	)
	if err != nil {
		log.Fatalf("failed to add job: %v\n", err)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	s.Start()
	log.Println("app started, waiting for signal...")

	<-ctx.Done()
}

func newGitlabClient(baseURL, token string) (*gitlab.Client, error) {
	return gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
}

func listOpenMRs(client *gitlab.Client, projectID string) ([]*gitlab.BasicMergeRequest, error) {
	ctx := context.Background()

	mrs, _, err := client.MergeRequests.ListProjectMergeRequests(
		projectID,
		&gitlab.ListProjectMergeRequestsOptions{
			State: gitlab.Ptr("opened"),
			ListOptions: gitlab.ListOptions{
				PerPage: 50,
				Page:    1,
			},
		},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return nil, err
	}

	return mrs, nil
}

func postToSlackChannel(webhookURL, text string) error {
	payload := map[string]string{
		"text": text,
	}

	bs, _ := json.Marshal(payload)

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(bs))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook status: %s", resp.Status)
	}

	return nil
}

func humanDuration(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		warn := ""
		if days > 2 {
			warn = "❓"
		}
		if days > 3 {
			warn = "😳"
		}
		if days > 5 {
			warn = "💀"
		}

		if hours == 0 {
			return fmt.Sprintf("%dd %s", days, warn)
		}
		return fmt.Sprintf("%dd %dh %s", days, hours, warn)
	}
}

func formatMRsForSlack(mrs []*gitlab.BasicMergeRequest) string {
	if len(mrs) == 0 {
		return "✅ No open merge requests"
	}

	var b strings.Builder
	b.WriteString("*Open Merge Requests:*\n")

	slices.SortFunc(mrs, func(a, b *gitlab.BasicMergeRequest) int {
		if a == nil || b == nil {
			return 0
		}

		if a.CreatedAt.Before(*b.CreatedAt) {
			return -1
		} else if a.CreatedAt.After(*b.CreatedAt) {
			return 1
		}
		return 0
	})

	for _, mr := range mrs {
		createdAt := time.Time{}
		if mr.CreatedAt != nil {
			createdAt = *mr.CreatedAt
		}

		liveTime := "unknown"
		if !createdAt.IsZero() {
			liveTime = humanDuration(createdAt)
		}

		author := "unknown"
		if mr.Author != nil {
			author = mr.Author.Name
		}

		b.WriteString(fmt.Sprintf(
			"• <%s|%s> - %s *%s*\n",
			mr.WebURL,
			mr.Title,
			author,
			liveTime,
		))
	}

	return b.String()
}

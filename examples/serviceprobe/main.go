// Example service probe scanner demonstrating service discovery with the Rediver SDK.
// Runs in worker mode: polls for jobs continuously until interrupted.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/joho/godotenv"

	"github.com/califio/rediver-sdk-go"
)

func main() {
	godotenv.Load()

	scanner := rediver.NewScanner("service_probe",
		[]rediver.TargetType{rediver.TargetTypeDomain, rediver.TargetTypeIP, rediver.TargetTypeService},
		discoverServices,
		rediver.WithRetestHandler(retestServices),
		rediver.WithParam(
			rediver.StringParam("ports").
				Label("Ports").
				Description("Ports to scan (e.g., '80,443,8080' or '1-1000')").
				Default("80,443,8080,8443").
				Build(),
		),
		rediver.WithParam(
			rediver.IntParam("timeout").
				Label("Timeout").
				Description("Connection timeout in seconds").
				Default(5).
				Build(),
		),
		rediver.WithParam(
			rediver.BoolParam("grab_banners").
				Label("Grab Banners").
				Description("Attempt to grab service banners").
				Default(true).
				Build(),
		),
	)

	runner, err := rediver.NewRunner(
		os.Getenv("REDIVER_URL"),
		os.Getenv("REDIVER_TOKEN"),
		rediver.WithWorkerMode(),
		rediver.WithAgentIDPath("/tmp/rediver-serviceprobe"),
	)
	if err != nil {
		log.Fatalf("create runner: %v", err)
	}

	if err := runner.Add(scanner); err != nil {
		log.Fatalf("register: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}

	log.Println("task complete, token revoked")
}

func discoverServices(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	ports := job.Param("ports").StringOr("80,443,8080,8443")
	timeout := job.Param("timeout").IntOr(5)
	grabBanners := job.Param("grab_banners").BoolOr(true)

	logger.Info("starting service probe", "ports", ports, "timeout", timeout, "banners", grabBanners)
	var services []rediver.Service
	for _, target := range job.Domains() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		host := target.Value
		services = append(services,
			rediver.Service{
				Host: host, Port: 80, ServiceName: "http",
				HTTP: &rediver.HTTPInfo{
					URL: fmt.Sprintf("http://%s", host), Scheme: "http",
					StatusCode: 200, Title: "Welcome Page", Webserver: "nginx/1.24.0",
				},
			},
			rediver.Service{
				Host: host, Port: 443, ServiceName: "https",
				Certificate: &rediver.TLSInfo{
					SubjectCN: host, IssuerCN: "Let's Encrypt Authority X3",
					IssuerOrg: "Let's Encrypt", NotBefore: "2024-01-01T00:00:00Z",
					NotAfter: "2024-04-01T00:00:00Z",
				},
				HTTP: &rediver.HTTPInfo{
					URL: fmt.Sprintf("https://%s", host), Scheme: "https",
					StatusCode: 200, Title: "Secure Page", Webserver: "nginx/1.24.0",
					Technologies: []string{"React", "Node.js"},
				},
			},
			rediver.Service{
				Host: host, Port: 22, ServiceName: "ssh",
				CPEs: []string{"cpe:/a:openbsd:openssh:8.9"},
			},
		)

		time.Sleep(100 * time.Millisecond)
	}

	logger.Info("probe complete", "services_found", len(services))
	emit(rediver.Services(services...))
	return nil
}

func retestServices(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	services := job.Services()
	logger.Info("starting recheck", "services_count", len(services))

	var active []rediver.Service
	for _, svc := range services {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result := rediver.Service{Host: svc.Host, Port: svc.Port}

		if svc.URL != "" {
			result.ServiceName = "https"
			result.HTTP = &rediver.HTTPInfo{
				URL: svc.URL, StatusCode: 200,
			}
		}

		active = append(active, result)
		time.Sleep(50 * time.Millisecond)
	}

	emit(rediver.Services(active...))
	return nil
}

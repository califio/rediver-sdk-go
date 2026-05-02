package rediver

// Integration holds third-party integration tokens for the job.
type Integration struct {
	CloudflareTokens []string
}

// Repository contains git repository information for CI/SAST jobs.
// ArtifactID is non-empty when the source code is delivered as a pre-uploaded
// artifact (tar.gz) rather than a live git clone.
type Repository struct {
	URL           string
	Provider      string // "gitlab" | "github" — scanner may behave differently per provider
	Event         string // "push" | "merge_request" | "pull_request"
	Ref           string // raw ref: "refs/heads/main", "refs/tags/v1.0"
	Branch        string
	CommitSHA     string
	BaseBranch    string
	BaseCommitSHA string
	PrNumber      int
	ArtifactID    string `json:"artifact_id"`
	DiffOnly      bool   // true = scan changed files only
	Username      string
	Password      string
}

// DomainTarget contains domain info from the job target.
type DomainTarget struct {
	ID    string
	Value string
	CNAME string
	IPs   []string
}

// IPTarget contains IP address info from the job target.
type IPTarget struct {
	ID    string
	Value string
}

// SubnetTarget contains subnet info from the job target.
type SubnetTarget struct {
	ID    string
	Value string
}

// ServiceTarget contains service endpoint info from the job target.
type ServiceTarget struct {
	ID    string
	Value string
	Host  string
	Port  int
	URL   string
}

// Domains returns domain targets from the job.
func (j *job) Domains() []DomainTarget {
	if j.detail == nil || j.detail.Target == nil {
		return nil
	}
	domains := j.detail.Target.GetDomains()
	result := make([]DomainTarget, 0, len(domains))
	for _, d := range domains {
		dt := DomainTarget{
			Value: d.GetValue(),
			IPs:   d.GetIps(),
		}
		if id := d.GetId(); id != "" {
			dt.ID = id
		}
		if cn := d.GetCname(); cn != "" {
			dt.CNAME = cn
		}
		result = append(result, dt)
	}
	return result
}

// IPs returns IP targets from the job.
func (j *job) IPs() []IPTarget {
	if j.detail == nil || j.detail.Target == nil {
		return nil
	}
	ips := j.detail.Target.GetIps()
	result := make([]IPTarget, 0, len(ips))
	for _, ip := range ips {
		it := IPTarget{Value: ip.GetValue()}
		if id := ip.GetId(); id != "" {
			it.ID = id
		}
		result = append(result, it)
	}
	return result
}

// Subnets returns subnet targets from the job.
func (j *job) Subnets() []SubnetTarget {
	if j.detail == nil || j.detail.Target == nil {
		return nil
	}
	subnets := j.detail.Target.GetSubnets()
	result := make([]SubnetTarget, 0, len(subnets))
	for _, s := range subnets {
		st := SubnetTarget{Value: s.GetValue()}
		if id := s.GetId(); id != "" {
			st.ID = id
		}
		result = append(result, st)
	}
	return result
}

// Services returns service targets from the job.
func (j *job) Services() []ServiceTarget {
	if j.detail == nil || j.detail.Target == nil {
		return nil
	}
	services := j.detail.Target.GetServices()
	result := make([]ServiceTarget, 0, len(services))
	for _, s := range services {
		st := ServiceTarget{
			Value: s.GetValue(),
			Host:  s.GetHost(),
			Port:  int(s.GetPort()),
			URL:   s.GetUrl(),
		}
		if id := s.GetId(); id != "" {
			st.ID = id
		}
		result = append(result, st)
	}
	return result
}

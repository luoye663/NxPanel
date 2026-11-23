package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/update"
)

type githubModuleRelease struct {
	Tag         string     `json:"tag_name"`
	Draft       *bool      `json:"draft"`
	Prerelease  *bool      `json:"prerelease"`
	PublishedAt *time.Time `json:"published_at"`
}

func (release githubModuleRelease) stable() bool {
	return release.Draft != nil && !*release.Draft && release.Prerelease != nil && !*release.Prerelease &&
		release.PublishedAt != nil && !release.PublishedAt.IsZero() && nginxmodule.ValidTag(release.Tag)
}

func (source moduleSource) storeReleases(ctx context.Context, repo, endpoint string, output any) error {
	body, err := source.fetch(ctx, "repos/"+repo+"/releases"+endpoint+".json", 4<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("模块服务 Release 响应无效")
	}
	return nil
}

func (source moduleSource) releasePage(ctx context.Context, entry nginxmodule.IndexEntry, page int) (nginxmodule.ReleasePage, error) {
	result := nginxmodule.ReleasePage{Tags: []string{}}
	var releases []githubModuleRelease
	if err := source.storeReleases(ctx, entry.Repository, "/pages/"+strconv.Itoa(page), &releases); err != nil {
		return result, err
	}
	if releases == nil || len(releases) > nginxmodule.ReleasePageSize {
		return result, fmt.Errorf("模块 Release 分页响应无效")
	}
	seen := make(map[string]bool)
	for _, release := range releases {
		if release.stable() && !seen[release.Tag] {
			seen[release.Tag] = true
			result.Tags = append(result.Tags, release.Tag)
		}
	}
	if len(releases) == nginxmodule.ReleasePageSize && page < nginxmodule.MaxReleasePage {
		result.NextPage = page + 1
	}
	if page == 1 {
		var latest githubModuleRelease
		err := source.storeReleases(ctx, entry.Repository, "/latest", &latest)
		if err != nil && !errors.Is(err, errModuleResourceNotFound) {
			return result, err
		}
		if err == nil && latest.stable() {
			result.Latest = latest.Tag
			if !seen[latest.Tag] {
				result.Tags = append([]string{latest.Tag}, result.Tags...)
			}
		}
	}
	return result, nil
}

func (source moduleSource) releaseManifest(ctx context.Context, entry nginxmodule.IndexEntry, tag string) (nginxmodule.Manifest, moduleReceipt, error) {
	var published githubModuleRelease
	var receipt moduleReceipt
	if err := source.storeReleases(ctx, entry.Repository, "/tags/"+tag, &published); err != nil {
		return nginxmodule.Manifest{}, receipt, err
	}
	if !published.stable() || published.Tag != tag {
		return nginxmodule.Manifest{}, receipt, fmt.Errorf("所选版本不是已发布的正式 Release")
	}
	release := nginxmodule.Release{Repository: entry.Repository, Tag: tag}
	raw, err := source.fetch(ctx, nginxmodule.ManifestName, nginxmodule.MaxEnvelope, release)
	if err != nil {
		return nginxmodule.Manifest{}, receipt, err
	}
	signature, err := source.fetch(ctx, nginxmodule.ManifestSig, nginxmodule.MaxEnvelope, release)
	if err != nil {
		return nginxmodule.Manifest{}, receipt, err
	}
	manifest, err := nginxmodule.VerifyManifest(raw, signature, entry, tag, source.test)
	if err != nil {
		return nginxmodule.Manifest{}, receipt, err
	}
	return manifest, moduleReceipt{Tag: tag, Manifest: raw, Signature: signature}, nil
}

func (s *Server) handleNginxModuleReleases(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 || page > nginxmodule.MaxReleasePage || !nginxmodule.ValidID(id) {
		writeAgentError(w, http.StatusBadRequest, "无效的模块或分页参数")
		return
	}
	if !s.moduleMu.TryLock() {
		writeAgentError(w, http.StatusConflict, errModuleBusy.Error())
		return
	}
	defer s.moduleMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Second)
	defer cancel()
	feed, err := s.loadModuleFeed(ctx, false)
	if err != nil {
		writeAgentError(w, http.StatusBadGateway, err.Error())
		return
	}
	entry, ok := feed.entry(id)
	if !ok {
		writeAgentError(w, http.StatusNotFound, "模块未登记在可信索引中")
		return
	}
	pageData, err := s.moduleSource.releasePage(ctx, entry, page)
	if err != nil {
		writeAgentError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAgentOK(w, pageData)
}

func (s *Server) handleNginxModulePreflight(w http.ResponseWriter, r *http.Request) {
	id, tag := r.URL.Query().Get("id"), r.URL.Query().Get("tag")
	if !nginxmodule.ValidID(id) || !nginxmodule.ValidTag(tag) {
		writeAgentError(w, http.StatusBadRequest, "无效的模块或 Release tag")
		return
	}
	if !s.moduleMu.TryLock() {
		writeAgentError(w, http.StatusConflict, errModuleBusy.Error())
		return
	}
	defer s.moduleMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Second)
	defer cancel()
	offer, err := s.prepareModuleInstall(ctx, id, tag, false)
	result := nginxmodule.Preflight{Tag: tag, Version: offer.asset.Version, Runtime: offer.status.Runtime, Confirmation: offer.confirmation, Build: offer.buildStatus}
	if err != nil {
		result.Reason = err.Error()
	}
	writeAgentOK(w, result)
}

func (s *Server) prepareModuleInstall(ctx context.Context, id, tag string, fresh bool) (moduleOffer, error) {
	var offer moduleOffer
	if err := s.moduleSource.checkScriptJournal(); err != nil {
		return offer, err
	}
	feed, err := s.loadModuleFeed(ctx, fresh)
	if err != nil {
		return offer, err
	}
	entry, ok := feed.entry(id)
	if !ok {
		return offer, fmt.Errorf("模块未登记在可信索引中")
	}
	instance, version, managed, runtimeErr := s.moduleSnapshot(ctx)
	offer = s.describeModule(ctx, instance, version, entry.DisplayModule(), managed[id], false, true, runtimeErr)
	if offer.status.Action != "install" {
		reason := offer.status.Reason
		if reason == "" {
			reason = "模块已加载，切换版本前请先卸载"
		}
		return offer, errors.New(reason)
	}
	target, err := moduleTarget(instance, version)
	if err != nil {
		return offer, err
	}
	manifest, receipt, err := s.moduleSource.releaseManifest(ctx, entry, tag)
	if err != nil {
		return offer, err
	}
	asset, err := manifest.Match(target)
	if errors.Is(err, nginxmodule.ErrNoMatch) && manifest.Build != nil {
		request := observedBuildRequest(ctx, instance, target, manifest, receipt.Manifest)
		offer.buildRequest = &request
		status, statusErr := s.moduleSource.buildStatus(ctx, request, false)
		if statusErr != nil {
			return offer, statusErr
		}
		if status.State != "ready" {
			offer.buildStatus = &status
			return offer, fmt.Errorf("所选版本没有可安装成品")
		}
		var built nginxmodule.BuildResult
		var signed nginxmodule.SignedBuild
		built, signed, err = s.moduleSource.getBuild(ctx, request, manifest, receipt.Manifest, entry)
		if err == nil {
			asset, receipt.Build = built.Asset, &signed
		}
	}
	if err != nil {
		return offer, err
	}
	path := s.moduleSource.assetPath(id, asset.SHA256)
	if err := s.moduleActionReadiness(ctx, instance, true, id, path); err != nil {
		return offer, err
	}
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		return offer, err
	}
	offer.module, offer.asset, offer.target, offer.path, offer.receipt = manifest.Module, asset, target, path, receipt
	offer.identity = "install " + sha256Text(entryBytes) + " " + sha256Text(receipt.Manifest) + " " + asset.SHA256
	offer.confirmation = moduleConfirmation(instance, target, offer.identity)
	return offer, nil
}

func (s *Server) handleNginxModuleBuild(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID  string `json:"id"`
		Tag string `json:"tag"`
	}
	if update.Decode(http.MaxBytesReader(w, r.Body, 1024), &request) != nil || !nginxmodule.ValidID(request.ID) || !nginxmodule.ValidTag(request.Tag) {
		writeAgentError(w, http.StatusBadRequest, "无效的模块构建请求")
		return
	}
	if !s.moduleMu.TryLock() {
		writeAgentError(w, http.StatusConflict, errModuleBusy.Error())
		return
	}
	defer s.moduleMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Second)
	defer cancel()
	offer, _ := s.prepareModuleInstall(ctx, request.ID, request.Tag, true)
	if offer.buildRequest == nil || offer.buildStatus == nil || !offer.buildStatus.CanSubmit {
		writeAgentError(w, http.StatusConflict, "当前环境不能提交构建，请重新检查")
		return
	}
	status, err := s.moduleSource.buildStatus(ctx, *offer.buildRequest, true)
	if err != nil {
		writeAgentError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAgentOK(w, status)
}

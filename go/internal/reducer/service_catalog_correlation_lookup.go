package reducer

type serviceCatalogRepositoryLookup struct {
	activeByID  map[string][]serviceCatalogRepositoryEvidence
	staleByID   map[string][]serviceCatalogRepositoryEvidence
	activeByURL map[string][]serviceCatalogRepositoryEvidence
	staleByURL  map[string][]serviceCatalogRepositoryEvidence
}

func buildServiceCatalogRepositoryLookup(
	repositories []serviceCatalogRepositoryEvidence,
) serviceCatalogRepositoryLookup {
	lookup := serviceCatalogRepositoryLookup{
		activeByID:  make(map[string][]serviceCatalogRepositoryEvidence, len(repositories)),
		staleByID:   make(map[string][]serviceCatalogRepositoryEvidence),
		activeByURL: make(map[string][]serviceCatalogRepositoryEvidence, len(repositories)),
		staleByURL:  make(map[string][]serviceCatalogRepositoryEvidence),
	}
	for _, repository := range repositories {
		appendRepositoryByKey(lookup.byTombstoneID(repository.tombstone), repository.repositoryID, repository)
		appendRepositoryByKey(lookup.byTombstoneURL(repository.tombstone), canonicalPackageSourceURLKey(repository.remoteURL), repository)
	}
	return lookup
}

func (l serviceCatalogRepositoryLookup) byRepositoryID(
	repositoryID string,
) ([]serviceCatalogRepositoryEvidence, []serviceCatalogRepositoryEvidence) {
	return l.activeByID[repositoryID], l.staleByID[repositoryID]
}

func (l serviceCatalogRepositoryLookup) byCanonicalURL(
	canonicalURL string,
) ([]serviceCatalogRepositoryEvidence, []serviceCatalogRepositoryEvidence) {
	return l.activeByURL[canonicalURL], l.staleByURL[canonicalURL]
}

func (l serviceCatalogRepositoryLookup) byTombstoneID(
	tombstone bool,
) map[string][]serviceCatalogRepositoryEvidence {
	if tombstone {
		return l.staleByID
	}
	return l.activeByID
}

func (l serviceCatalogRepositoryLookup) byTombstoneURL(
	tombstone bool,
) map[string][]serviceCatalogRepositoryEvidence {
	if tombstone {
		return l.staleByURL
	}
	return l.activeByURL
}

func appendRepositoryByKey(
	index map[string][]serviceCatalogRepositoryEvidence,
	key string,
	repository serviceCatalogRepositoryEvidence,
) {
	if key == "" {
		return
	}
	index[key] = append(index[key], repository)
}

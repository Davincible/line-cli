package session

func prepareNativeStorage() error {
	store, err := resolvedLinuxStorage()
	if err != nil {
		return err
	}
	return store.Prepare()
}

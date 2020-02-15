package erconfig

func FindApplication(id string, apps []Application) *Application {
	for _, app := range apps {
		if app.Id == id {
			return &app
		}
	}

	return nil
}

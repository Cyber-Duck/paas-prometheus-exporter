# Installing

## Steps to deploy application.
- Edit these fields within the manifest:
  - username.
  - password.
  - api_endpoint.
      - location based UK/Ireland can be found in GOV PaaS Monitoring link.
  - Set Authentication login credentials so Grafana can talk to it.
- More configuraton options can be found below.

> Make sure you are pointed at the right location within CloudFoundry
> 
```cf target -o ORG -s SPACE```

> Push the application without starting to ensure deploys correctly.
> 
```cf push --no-start prometheus-exporter-APPNAME```

> Start the application and check endpoint asks for a login.

```cf start prometheus-exporter-APPNAME```


# Documentation Links

| where | Description| Link | 
| ---- | ----- | ---- |
| Cloud Foundry| Application deployment docs. |https://docs.cloudfoundry.org/devguide/deploy-apps/ | 
| Prometheus Exporter| Application to created metrics endpoint repository. |https://github.com/alphagov/paas-prometheus-exporter| 
| Manifest Docs| Additional Docs for manifest configuration |https://docs.developer.swisscom.com/devguide/deploy-apps/manifest.html| 
| GOV PaaS Monitoring | Instructions from Gov PaaS for application monitoring. | https://docs.cloud.service.gov.uk/monitoring_apps.html#use-the-paas-prometheus-exporter-app|
| Logit| Metrics and Logs Monitoring PaaS | https://logit.io|

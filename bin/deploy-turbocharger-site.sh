#!/bin/sh -eu

# This is a helper script for reusing Edgerouter Docker image with Deployer (see https://github.com/function61/deployer)
# to deploy a static website and make it live.

edgerouterAppId="$1"

# this command makes the site available in Turbocharger.
# we'll get a manifest ID which we need to point the site configuration at.
manifestId=$(cat site.tar.gz | gzip -d | edgerouter turbocharger tar-deploy-to-store "$edgerouterAppId")

# now make the site live
edgerouter turbocharger deploy-site-from-store "$edgerouterAppId" "$manifestId"

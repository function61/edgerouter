Managing S3 static websites
===========================

Contents:

- [Assumptions](#assumptions)
- [Create static website definition](#create-static-website-definition)
- [Deploy certificate for the website](#deploy-certificate-for-the-website)
- [Deploy new version of the site](#deploy-new-version-of-the-site)


Assumptions
-----------

We'll assume your S3 bucket name is `yourorg-staticwebsites` and its region is `eu-central-1`.

The website we're deploying is `example.com`. Its content is in `example.com.tar.gz`.

You can run these `$ edgerouter` commands from any computer (it doesn't have to be the
loadbalancer).


Create static website definition
--------------------------------

You need to do this only once. 

At the start, your service discovery might be empty:

```console
$ edgerouter discovery ls
+----+-----------+---------+
| ID | Frontends | Backend |
+----+-----------+---------+
+----+-----------+---------+
```

Now, let's create definition for the site:

```console
$ edgerouter s3 mk example.com example.com / fn61-staticwebsites eu-central-1
$ edgerouter discovery ls
+-------------+-----------------------+--------------------+
| ID          | Frontends             | Backend            |
+-------------+-----------------------+--------------------+
| example.com | hostname:example.com/ | s3_static_website: |
+-------------+-----------------------+--------------------+
```


Deploy certificate for the website
----------------------------------

You need to do this only once. This step is done from CertBus:

```console
$ certbus cert mk example.com
```

Done! Edgerouter has picked up the certificate, and CertBus will renew it automatically
when needed.


Deploy new version of the site
------------------------------

You do this same step:

- the first time you upload your site
- each time you update your site

Your website content is in `example.com.tar.gz`. The version string can be anything you want,
(e.g. `v1`) but we recommend fetching the version automatically from version control.

Now let's upload/update the site:

```console
$ edgerouter s3 deploy example.com v1 example.com.tar.gz
... upload progress output ...
$ edgerouter discovery ls
+-------------+-----------------------+----------------------+
| ID          | Frontends             | Backend              |
+-------------+-----------------------+----------------------+
| example.com | hostname:example.com/ | s3_static_website:v1 |
+-------------+-----------------------+----------------------+
```

(Take note: service discovery now displays our version as atomically having been deployed.)

Done!

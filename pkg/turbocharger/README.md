Turbocharger
============

"Learn this one weird trick to CDN-enable your applications."

Allows your loadbalancer to know exactly when (and which) files are changed due to the a URL mount
point describing its sub-tree with an immutable manifest identifier.
When any file changes, the manifest changes. The manifest has immutable identifiers for each file
version, so even when the manifest changes we can transfer only the changed files.


## Design principles

- Suitable for hosting both:
	* static websites and
	* hosting static subtrees of dynamic web apps (be it Docker container, AWS Lambda function etc.)
- Make the minimum amount of changes to an end-application to enable very aggressive caching.
- Progressive enhancement. The same web-app must be runnable without turbocharger-aware loadbalancing
  and it should still work.
- What if you could have the authority of your app's static asset serving even on AWS Lambda (which is
  not great for static file serving) but still have ridiculously great performance?
	* Looking at this from perspective of hybrid Lambda / traditional web servers (same HTTP server runs unchanged on Lambda).
- Deployments and serving can benefit from differential transfer (= only changed files uploaded to storage/downloaded to cache)
	* My 30.8 MB deploys used to take 48 s, now takes 13 s (with barely any data transferred as most
	  of the time goes into "exists" checks in the CAS)
- Content-addressability ❤️


## Design properties

- Very aggressive caching capabilities
- Serve gzip'd content that is actually gzip'd at cache level, so we need to only compress each file once
- Static sites are served atomically, but we still get differential transfers to backing store
  (no need to upload the full tree each time)
- Hybrid dynamic/static apps (dynamic web app with sub-tree e.g. /static being static) should work
  without turbocharger
- Minimal changes to hybrid apps to enable turbocharging. request to `/static/main.js` returns
  header `turbocharger: 60303ae22b998861 /static`. The form is `turbocharger: <manifest ID> <tree>`.
- Above reads "everything under `/static` is found from manifest `60303ae22b998861` in CAS"
- Loadbalancer only needs to download immutable manifest `60303ae22b998861` once. It contains mappings
  like `{"/main.js": "fd61a03af4f77d87", "/images/3.jpg": "a4e624d686e03ed2"}` which are yet again found from CAS.
- We don't even have to pass would-be-404s to origin, since we know whether paths exist or not based on the manifest.
- Support custom 404 pages


## Deploy command

```console
$ cat site.tar.gz | gzip -d | edgerouter turbocharger tar-deploy-to-store joonas.fi-blog
```

The command gave you a manifest ID `QSA90-KjEwnNaPn2qdlo6cHJQeSazX_A1eizwOAl_fM`

Edgerouter app definition looks like this:

```json
{
  "id": "joonas.fi",
  "frontends": [
    {
      "kind": "hostname",
      "hostname": "joonas.fi",
      "path_prefix": "/"
    }
  ],
  "backend": {
    "kind": "turbocharger",
    "turbocharger_opts": {
      "manifest": "QSA90-KjEwnNaPn2qdlo6cHJQeSazX_A1eizwOAl_fM"
    }
  }
}
```

When a new version of the website has been deployed to Turbocharger, we only update the manifest ID in Edgerouter.

A rollback, should you need one, is exactly as easy as just reverting to an old manifest ID.


## Acceleration to Lambda static files

tl;dr 10 500 k reqs/s vs 26 reqs/s
With:

```console
$ hey -z 15s https://happppppy.dev.fn61.net/happy/static/images/MAHj.jpg

Summary:
  Total:        15.0112 secs
  Slowest:      0.1397 secs
  Fastest:      0.0003 secs
  Average:      0.0048 secs
  Requests/sec: 10513.5114

Response time histogram:
  0.000 [1]      |
  0.014 [152975] |■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■
  0.028 [4686]   |■
  0.042 [100]    |
  0.056 [8]      |
  0.070 [0]      |
  0.084 [2]      |
  0.098 [7]      |
  0.112 [20]     |
  0.126 [20]     |
  0.140 [1]      |
```

Without:

```console
$ hey -z 15s https://happppppy.dev.fn61.net/happy/static/images/MAHj.jpg

Summary:
  Total:        16.5229 secs
  Slowest:      2.5380 secs
  Fastest:      0.1106 secs
  Average:      1.9107 secs
  Requests/sec: 26.1456

Response time histogram:
  0.111 [1]   |
  0.353 [6]   |■
  0.596 [2]   |
  0.839 [2]   |
  1.082 [10]  |■■
  1.324 [27]  |■■■■■■
  1.567 [95]  |■■■■■■■■■■■■■■■■■■■■■■
  1.810 [9]   |■■
  2.052 [13]  |■■■
  2.295 [169] |■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■■
  2.538 [98]  |■■■■■■■■■■■■■■■■■■■■■■■
```

Caveat: my internet download speed (100 Mbps) might have played some part in this.


## Under the hood

### CAS

Turbocharger stores files in content-addressable storage (CAS). 

Loadbalancer middleware is a special case by having two distinct storages. One file belongs in either one,
depending on whether the file is compressible. HTML/JS/CSS files are compressible, JPEG/GIF/MP4 not etc.

```console
$ tree /var/cache/turbocharger
/var/cache/turbocharger
|-- gzipped
|   |-- -Kdyyv6A-qJczcMAbOJ3QH6JBeLrPGbxA3V1H7sjbuQ
|   |-- 2c0kmvbEI6ntwS7HcgtfPCfgk52Q5Qn43sD72Y0kjWQ
|   |-- 9BwJGZOvKmtxbyNLY64_RSjWziDadkYQMPQU8mkYTCk
|   |-- CSXorXvZcTkaix6Yvo6HppcZGetbYMGWSFlBw8HfCJo
|   |-- DUjVq194puHcF9uJS31YnKLU1-vHpEQaj6xTJo5ue0o
|   |-- GG6rubuufpiDgU2xzUPaRhRmGCJpTlVpOSuDe5rIaa0
|   |-- IwP_WbN-9OLn-aXlzR1Nqxr_yXxOaViWlqjCcmei3ik
|   |-- KDOGdXu9LEP88Sxk09iFj1c-mSGFBnjBRvbLWtAkVnY
|   |-- Li4njatBsm77LsXhZKhxkcnLJOEddOrzL8YE1NsQcGc
|   |-- M0OtarCfRjuLJHJn-1qwc4e5LPkll0xRpnGPNnIPxSU
|   |-- Uhl3arhfll5-lvXPUL6FR-Y9oqdOgdVp7TL-XBJXI8o
|   |-- gsTkEqAR2lRZv_JJkuCQXa2IUvJbcSr2VkytNYJfRe8
|   |-- mK6SlQMxtVBpMfKqk-2X-n2Dg5N5FSRDsqztxiPTq68
|   |-- oA6JKKCKPrBXtUnZfZLQnMX-PpEqH21JMgzzNIG76Jk
|   |-- slxc7pGfBj7mDISbKaDfHYnLA0CZZcBgWyF10xUA6rA
|   |-- ybRkN9dBjhcS2qrW1z-hfCxq-1aBdwyQM5wlQoQVt_0
|   `-- zBZVpaScPA3I81AlWXXmC-2UidCMAzLbMDJ4pHUceoY
`-- uncompressed
    |-- -AugZmBihv6ryHzU9qEbv2uAkU5EyZ-VJ7FY5X5BILU
    |-- 0lqgykAFIVyZde2jKai35I5hlYRzFFi82onmAjjIjd8
    |-- 1kjbiRFLQYABs011bYXYzCBgl1xb7wtcQNfRmKkMRPc
    |-- 47DEQpj8HBSa-_TImW-5JCeuQeRkm5NMpJWZG3hSuFU
    |-- 4zvX0_PJR9K9JUqQW3hib6m8pCeGWtCW-YNAPvWtZdM
    |-- 8HrzjmBGMDYb5tMH7Svg1SdbLwqcG0rLbRc6G9S33pU
    |-- 94z4eqeLna1bJWa7v9UMrV41ulOXi5IC8lfXuD82LUQ
    |-- A4J8a_K-mbtR9IXn4Mlpn8UA7gfobM61u-t9ruuRYL0
    |-- A5PIVDzDbZTZSv5gR9B_oU00xhLOpEQtbDitW6UZul0
    |-- Aeo6B7DhmprNoCeMHk82HP__EjBKbFYTjtQx1kmZFX8
    |-- DEpHFvWeQDUH_-JVjhha1U7LV7U9kDuq8IyaXjzPd40
    |-- EbQ1G9b1wxGXFRMyds641xtOvdc2fhx4piqSQMLCoq8
    |-- HFyNSHGB6TmWdCmRLJ70NBbCsnoL9QomZePlhHN5Z9k
    |-- HkO_KF-BEeJKo1J2VRh62ThI2RKfUylhCEKMbCJqjhA
    |-- IeFmJKCp-fPdTnVYfSo-5QJeFDzl-5Q059B1yJldsyU
    |-- JQNr46zgAj6fNPTHE25V4GuEeZNKyMEYUy0O6ApEDOk
    |-- McJ01ZV4HsWk9uhHRA96pQSoYi1_5hGYT0_2vHwCoYg
    |-- NSUffPCW6GrMMbbJkFdafGTvHFOpCO8I-Niu3hguXsE
    |-- OAxSZYuYJjaoMrOZfjt6Kypp7_Fn0_3zrhI9KCqAJ40
    |-- PF-51x7x4jY7onUD8LgJB7NBOMMaCcmctTVRccYryBA
    |-- QSA90-KjEwnNaPn2qdlo6cHJQeSazX_A1eizwOAl_fM
    |-- R1D0OwzOwvdogt7--ryPsouhQC5kcAujcKPKfZriu4s
    |-- R1GeXefKquorSYYlErtheWTZtwTOC9-gquzbo4Kpskc
    |-- RuJoSi-45h2BmDjWQfyl9EQCZ34sQjkmdQBR_eD8EBY
    |-- S4olAlrNWKBzGYpE9Y0u_uE6_xSKPj3kuxRiPuZNDY0
    |-- V23ek1ZBLQDPnZMbt6YXAfCnha6GAS3D9y254ZHRS6c
    |-- X6fxglqnVDW0RHvM-yy3d5g8Sb6w_keuAeqce-MUWsQ
    |-- YjvTZDJ-0DLqlEhKnWT40_gGQWnaD_JxuHBBZnLtFLw
    |-- ZP_4gzwct-cqbg9MMDKr-TuSCAoLCYLvfH3KIrptYxs
    |-- _2H6cgAe-J7r7CcVzjM4yEzPikyPuQDpdmI1Pf13SkY
    |-- ag1Xjf-QOF4hmVbQXqNWKB72dVUzQVtcWpclRUX7lAM
    |-- eH12rW3qtnzPi6wbWEJgIF4RT1CPxVQrYS4_ddSaNOQ
    |-- jUpJ0sVGq8urLeQBxNUSHsGI6pMY41HtXOcByxxUz-4
    |-- jkVgwWx5cO-kdoBFCyzyOdSkgsBW0wis6hK7kCKQbIs
    |-- lbOk-pUdHiag_OdxsTvMAk1FgWPBmXullZie5KtYjv4
    |-- mWT4lKrQH2fUKTb8w-DDMCxsnPKME6AGe6RIzNZOX2Y
    |-- ma4OtCOO0loJ3iyOha4I-BHi_IOFFTwnsR4HVQTTTH8
    |-- mploPCZGhbVOMGg547pc7_-43YU9JLOcoHCCJHWvY5E
    |-- nLxKJbyCuOIulOQUfiXY9q83qWeOCr8DPn5Cm5Yz4-0
    |-- pd1RvRGe4HuU6GPpfdzsNTZ61jDaqu5j9yRnbm78B3o
    |-- sX_liXc11SeIJFKnclqTS4yfSqhFdz2YZCo8amATJfY
    |-- uPWpWwnXtAHTg5RcG_059gDPqbiTkdsKclcZ4BlHdek
    |-- upejB3TL0OoPLTg0dJJKHd9yIzu6vOJvNhSPmcqDSu8
    `-- za9gb2ASJDqlvo9V3Bb9jiexP0fn8rj_MEe_jYM0PLs
```

# Changelog

## 0.1.0 (2026-11-23)


### ⚠ BREAKING CHANGES

* **users:** drop the users kind

### Features

* **api:** the LogtoConfig schema, logto.aliud.fr/v1alpha1 ([372928e](https://github.com/Aliud-Corp/logto-operator/commit/372928e95bffbfdb9adb2f2566d5c6e1b8fc7b40))
* **api:** vendor the Logto 1.44.0 Management API spec ([c5ffbe1](https://github.com/Aliud-Corp/logto-operator/commit/c5ffbe1d559977b5fba015565b31de5915bfc28b))
* **api:** vendor the Logto support window, 1.44.0 to 1.46.0 ([8c599fe](https://github.com/Aliud-Corp/logto-operator/commit/8c599fe1b7b0b6e9f8cb139c65390c69332fc413))
* **cli:** add the logtoctl stub and task build ([cf8111c](https://github.com/Aliud-Corp/logto-operator/commit/cf8111c284b322fcc1e942ac171af130caf1da75))
* **client:** the only door to Logto's Management API ([1811402](https://github.com/Aliud-Corp/logto-operator/commit/18114024e9660e0130dc348b199c3bd5b394abf1))
* **cli:** logtoctl render, plan, check and apply ([ad9f880](https://github.com/Aliud-Corp/logto-operator/commit/ad9f880330f67d3d77f950ec10019833e5f8cda0))
* **cli:** read the document on stdin with -f - ([97d6bce](https://github.com/Aliud-Corp/logto-operator/commit/97d6bcefe723c199bdab5c7c9faf6b40663ea69f))
* **cli:** read the document on stdin with -f - ([07375a4](https://github.com/Aliud-Corp/logto-operator/commit/07375a47c7b679277aac8faefe6735e54b1884e3))
* **cli:** recognise the Logto version, refuse an untested one ([bde5a88](https://github.com/Aliud-Corp/logto-operator/commit/bde5a88f0db6f5b1ac29e7f29fb85cb5f3a6f119))
* **config:** check a document against its JSON Schema before any call ([7ac17ab](https://github.com/Aliud-Corp/logto-operator/commit/7ac17ab4b309e7260b7dec6f0bb909c3cb6ec6ce))
* **config:** load and validate a LogtoConfig document ([a0c342b](https://github.com/Aliud-Corp/logto-operator/commit/a0c342b58c744645308b63d1e0f3e2d2dd63f9a7))
* **contract:** validate wire types against every supported Logto ([b5d1bcd](https://github.com/Aliud-Corp/logto-operator/commit/b5d1bcdb8bf99721525eed85b2351183f8903062))
* **engine:** deletes last, per-op results, typed failures, secrets read by apply only ([1305f22](https://github.com/Aliud-Corp/logto-operator/commit/1305f22667152de6363e879ca3bee266ed2eaddf))
* **engine:** kinds in dependency order, plan, apply and read-back ([4e4f28e](https://github.com/Aliud-Corp/logto-operator/commit/4e4f28e2e31b13f5beca3ad5cdb919a0df11b810))
* **kinds:** application-level admin users ([1eb6427](https://github.com/Aliud-Corp/logto-operator/commit/1eb642723294a222fba2feac28f739ff5eaa49fc))
* **kinds:** applications, their M2M secret and their roles ([78681a0](https://github.com/Aliud-Corp/logto-operator/commit/78681a08d4a4cdb15d095e4c2de1ad0c93af8835))
* **kinds:** custom profile fields ([ccd4ab3](https://github.com/Aliud-Corp/logto-operator/commit/ccd4ab345bad06640e1832667466d322c68486d8))
* **kinds:** organization scopes and organization roles ([3ba7643](https://github.com/Aliud-Corp/logto-operator/commit/3ba764329f274d384a45d6287eac775ac10526b5))
* **kinds:** resources and their scopes ([97e9938](https://github.com/Aliud-Corp/logto-operator/commit/97e9938fd240a36da7af721b521929841032a91e))
* **kinds:** the sign-in experience ([e8d9e6a](https://github.com/Aliud-Corp/logto-operator/commit/e8d9e6a1f46c0f9d2664e625596d96ae7036f578))
* **kinds:** the SMTP connector ([53e9b97](https://github.com/Aliud-Corp/logto-operator/commit/53e9b97d33d736931fb28e370d8aeec6eec29239))
* **mgmt:** generate the Management API client from the spec ([55d3724](https://github.com/Aliud-Corp/logto-operator/commit/55d37245fbc0a0d2beb728d38dcb2c9a8170c555))
* **operator:** add the install manifests and the ko image ([63b04ea](https://github.com/Aliud-Corp/logto-operator/commit/63b04eab190c54ed705d80fa41d32add1bdef764))
* **operator:** reconcile LogtoConfig objects with the engine ([0a69952](https://github.com/Aliud-Corp/logto-operator/commit/0a69952c2ecb346c3420312f951d593fe0939763))
* **operator:** reconcile LogtoConfig objects with the engine ([a46c01e](https://github.com/Aliud-Corp/logto-operator/commit/a46c01e8dddb9c7cfff526e3b24c4feb1fa7b197))
* **outputs:** ids printable, secrets only to a 0600 file or a descriptor ([121d37e](https://github.com/Aliud-Corp/logto-operator/commit/121d37efbf9ff994386e8da70af2906841f3d663))
* **plan:** the plan model and its rendering ([022e988](https://github.com/Aliud-Corp/logto-operator/commit/022e9888e4d8a2c7f9a572c397b113d9535a59e6))
* **roles:** global roles, granted by the same document ([a5a9dfc](https://github.com/Aliud-Corp/logto-operator/commit/a5a9dfcea2dcd50921d9da18515592358210f1a2))
* **secrets:** resolve valueFrom sources, never print a secret ([79f9e5c](https://github.com/Aliud-Corp/logto-operator/commit/79f9e5c4cb60a06f8b5f17724fee481035271d41))
* **tools:** specdiff, to judge a new Logto minor by a diff ([55959e7](https://github.com/Aliud-Corp/logto-operator/commit/55959e7258533c138987e33d5062c8a4afd3bfc1))


### Bug Fixes

* **client:** never replay a POST or a PATCH on a server error ([247cc1b](https://github.com/Aliud-Corp/logto-operator/commit/247cc1ba2c23a10daaea38e5ef51b349e0de5fc9))
* **config:** read a fromDir template's body next to the template ([0f2eaba](https://github.com/Aliud-Corp/logto-operator/commit/0f2eaba10962c85354b4c399e94a5313737fa402))
* **config:** read a fromDir template's body next to the template ([be85ac1](https://github.com/Aliud-Corp/logto-operator/commit/be85ac1eb45fd7473d442b870641af9f55914e4f))
* **connectors:** show a template change by usage type, field by field ([da0dc98](https://github.com/Aliud-Corp/logto-operator/commit/da0dc988def8312d798e7ae43d03793c8d12c208))
* **connectors:** show a template change by usage type, field by field ([574c5ad](https://github.com/Aliud-Corp/logto-operator/commit/574c5ada574163462ecf014e91ded85a97867173))
* **contract:** check a union branch by branch ([c14e836](https://github.com/Aliud-Corp/logto-operator/commit/c14e836617fad84736bcf76b07e0e8d013da0cf9))
* **kinds:** pruned scopes are deletes, every write in Needs ([83a4bb2](https://github.com/Aliud-Corp/logto-operator/commit/83a4bb21cae19aa5871212dd773b26bc3fd79517))
* **kinds:** spec.prune.keep by name, indicator or id, tested per kind ([705f3bc](https://github.com/Aliud-Corp/logto-operator/commit/705f3bcf7543c2da86b68f38bebdede12a90ef5a))
* **kinds:** what Logto holds beside a declared value survives ([8e3c483](https://github.com/Aliud-Corp/logto-operator/commit/8e3c48309359251bfd083aa34ed97d02a3a71524))
* **operator:** the review of the controller ([30fb3dd](https://github.com/Aliud-Corp/logto-operator/commit/30fb3dd5e5a409c5ff8f4248bb57deef363577ca))
* **operator:** the second review of the controller ([26a4867](https://github.com/Aliud-Corp/logto-operator/commit/26a4867f590cf2dd00049acd573c0dfab1f08f01))
* **roles:** spare granted roles, keep Management API scopes ([82cf985](https://github.com/Aliud-Corp/logto-operator/commit/82cf98559affc32e576c5f181b981ecc3697ce2d))
* **specdiff:** a renamed operation is breaking, used.txt matches the code ([bd649c6](https://github.com/Aliud-Corp/logto-operator/commit/bd649c660036c1f38455ae6ee9200d6d253912e3))
* **specnorm:** leave Logto's Validator function fields unconstrained ([a2cb55e](https://github.com/Aliud-Corp/logto-operator/commit/a2cb55e77fa21124a24a9c6ab2eb829f32210244))


### Performance Improvements

* fingerprints only in the binary, normalized specs for the tests ([9590256](https://github.com/Aliud-Corp/logto-operator/commit/9590256d7e33bb22f2149e9da9410ba5ced29352))
* **test:** parse a served spec once, skip validating the spec routes ([5b841e9](https://github.com/Aliud-Corp/logto-operator/commit/5b841e9a866f44372997ec2173f3e441098dbfc2))


### Code Refactoring

* **users:** drop the users kind ([af02268](https://github.com/Aliud-Corp/logto-operator/commit/af02268fdfe0ffea8d2b7ec5905cba2401e945f7))

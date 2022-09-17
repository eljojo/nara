{
  mqtt = {
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "0d1khsry5mf63y03r6v91f4vrbn88277ksv7d69z3xmqs9sgpri9";
      type = "gem";
    };
    version = "0.5.0";
  };
  multi_json = {
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "0pb1g1y3dsiahavspyzkdy39j4q377009f6ix0bh1ag4nqw43l0z";
      type = "gem";
    };
    version = "1.15.0";
  };
  mustermann = {
    dependencies = ["ruby2_keywords"];
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "0m70qz27mlv2rhk4j1li6pw797gmiwwqg02vcgxcxr1rq2v53rnb";
      type = "gem";
    };
    version = "2.0.2";
  };
  nio4r = {
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "0xk64wghkscs6bv2n22853k2nh39d131c6rfpnlw12mbjnnv9v1v";
      type = "gem";
    };
    version = "2.5.8";
  };
  puma = {
    dependencies = ["nio4r"];
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "0qzq0c791kacv68hgk9zqsd1p7zx1y1rr9j10rn9yphibb8jj436";
      type = "gem";
    };
    version = "5.6.5";
  };
  rack = {
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "0axc6w0rs4yj0pksfll1hjgw1k6a5q0xi2lckh91knfb72v348pa";
      type = "gem";
    };
    version = "2.2.4";
  };
  rack-protection = {
    dependencies = ["rack"];
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "169jzzgvbjrqmz4q55wp9pg4ji2h90mggcdxy152gv5vp96l2hgx";
      type = "gem";
    };
    version = "2.2.2";
  };
  ruby2_keywords = {
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "1vz322p8n39hz3b4a9gkmz9y7a5jaz41zrm2ywf31dvkqm03glgz";
      type = "gem";
    };
    version = "0.0.5";
  };
  sinatra = {
    dependencies = ["mustermann" "rack" "rack-protection" "tilt"];
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "0mbjp75dy35q796iard8izsy7gk55g2c3q864r2p13my3yjmlcvz";
      type = "gem";
    };
    version = "2.2.2";
  };
  sinatra-contrib = {
    dependencies = ["multi_json" "mustermann" "rack-protection" "sinatra" "tilt"];
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "0359q8jc07bz766ninf2ll7fkvzqwryyc81vkdw6ayw45fwz3ia9";
      type = "gem";
    };
    version = "2.2.2";
  };
  tilt = {
    groups = ["default"];
    platforms = [];
    source = {
      remotes = ["https://rubygems.org"];
      sha256 = "186nfbcsk0l4l86gvng1fw6jq6p6s7rc0caxr23b3pnbfb20y63v";
      type = "gem";
    };
    version = "2.0.11";
  };
}

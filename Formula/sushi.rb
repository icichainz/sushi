# Homebrew formula for the sushi command. Install with:
#   brew install icichainz/sushi/sushi
# (Homebrew finds this file through the repository itself, so no separate
# tap repository is needed.)
class Sushi < Formula
  desc "Terminal file manager for macOS"
  homepage "https://github.com/icichainz/sushi"
  url "https://github.com/icichainz/sushi/archive/refs/tags/v0.4.0.tar.gz"
  sha256 "5b22350b484ae72700eb3bd17d0fddb37ca0b312d6622c3d824e93315b42405e"
  license "MIT"
  head "https://github.com/icichainz/sushi.git", branch: "main"

  depends_on "go" => :build
  depends_on :macos

  def install
    system "go", "build", "-trimpath", "-ldflags", "-s -w -X main.version=#{version}", "-o", bin/"sushi", "."
  end

  test do
    assert_match "sushi #{version}", shell_output("#{bin}/sushi --version")
  end
end

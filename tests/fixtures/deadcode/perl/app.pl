use strict;
use warnings;

package DeadCode::PerlFixture;

use Exporter qw(import);

our @EXPORT_OK = qw(public_perl_api);

BEGIN {
    direct_perl_helper();
}

sub new {
    my ($class) = @_;
    return bless {}, $class;
}

sub main {
    direct_perl_helper();
    selected_perl_handler();
    route_perl_root();
}

sub unused_perl_helper {
    return 'unused';
}

sub direct_perl_helper {
    return 'direct';
}

sub public_perl_api {
    return 'public';
}

sub route_perl_root {
    return direct_perl_helper();
}

sub selected_perl_handler {
    return direct_perl_helper();
}

sub generated_perl_stub {
    return 'generated';
}

sub dynamic_perl_dispatch {
    my ($name) = @_;
    no strict 'refs';
    return &{$name}();
}

sub AUTOLOAD {
    return;
}

sub DESTROY {
    return;
}

main();

#!/usr/bin/perl
use strict;
use warnings;

package EventSystem;

my %handlers;

sub register {
    my ($event, $callback) = @_;
    push @{$handlers{$event}}, $callback;
}

sub emit {
    my ($event, @args) = @_;
    if (exists $handlers{$event}) {
        for my $handler (@{$handlers{$event}}) {
            $handler->(@args);
        }
    }
}

sub create_logger {
    my ($prefix) = @_;
    return sub {
        my ($message) = @_;
        print "[$prefix] $message\n";
    };
}

sub compose {
    my @fns = @_;
    return sub {
        my ($value) = @_;
        for my $fn (@fns) {
            $value = $fn->($value);
        }
        return $value;
    };
}

1;

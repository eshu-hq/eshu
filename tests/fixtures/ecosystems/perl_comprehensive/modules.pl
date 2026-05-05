#!/usr/bin/perl
use strict;
use warnings;
use File::Basename;
use List::Util qw(sum reduce);
use Carp qw(croak confess);

package Utilities;

sub new {
    my ($class) = @_;
    return bless {}, $class;
}

sub format_path {
    my ($self, $path) = @_;
    return File::Basename::basename($path);
}

sub total {
    my ($self, @numbers) = @_;
    return List::Util::sum(@numbers);
}

sub validate {
    my ($self, $input) = @_;
    Carp::croak("Input required") unless defined $input;
    return 1;
}

1;
